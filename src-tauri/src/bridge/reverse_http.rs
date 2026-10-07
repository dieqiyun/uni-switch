use super::*;
use crate::types::Target;

fn claude_error(status: StatusCode, message: impl Into<String>) -> HttpError {
    (
        status,
        Json(
            json!({"type":"error","error":{"type":match status.as_u16() {
        400=>"invalid_request_error",401=>"authentication_error",403=>"permission_error",404=>"not_found_error",429=>"rate_limit_error",_=>"api_error"
    },"message":message.into()}}),
        ),
    )
}
fn authorize_claude(
    runtime: &Runtime,
    headers: &HeaderMap,
    target: &str,
    id: &str,
) -> std::result::Result<StoredProvider, HttpError> {
    let bearer = format!("Bearer {}", runtime.token);
    if headers.contains_key(header::ORIGIN)
        || !(headers
            .get(header::AUTHORIZATION)
            .and_then(|h| h.to_str().ok())
            == Some(bearer.as_str())
            || headers.get("x-api-key").and_then(|h| h.to_str().ok())
                == Some(runtime.token.as_str()))
    {
        return Err(claude_error(
            StatusCode::UNAUTHORIZED,
            "本地转换服务认证失败，请重新应用配置",
        ));
    }
    let target = match target {
        "claude_desktop" => Target::ClaudeDesktop,
        "claude_cli" => Target::ClaudeCli,
        _ => return Err(claude_error(StatusCode::NOT_FOUND, "转换目标不存在")),
    };
    runtime
        .store
        .lock()
        .map_err(|_| claude_error(StatusCode::SERVICE_UNAVAILABLE, "配置服务繁忙"))?
        .claude_bridge_provider(target, id)
        .map_err(|e| claude_error(StatusCode::CONFLICT, e.message))
}
fn openai_endpoint(base: &str, chat: bool) -> String {
    let mut base = base.trim_end_matches('/');
    for suffix in ["/chat/completions", "/responses"] {
        if let Some(root) = base.strip_suffix(suffix) {
            base = root;
            break;
        }
    }
    let resource = if chat {
        "chat/completions"
    } else {
        "responses"
    };
    if base.ends_with("/v1") {
        format!("{base}/{resource}")
    } else {
        format!("{base}/v1/{resource}")
    }
}
async fn send(
    runtime: &Runtime,
    provider: &StoredProvider,
    body: &Value,
    chat: bool,
) -> std::result::Result<reqwest::Response, HttpError> {
    let request = runtime
        .client
        .post(openai_endpoint(&provider.summary.base_url, chat));
    let request = if provider.summary.auth_mode == "x-api-key" {
        request.header("x-api-key", &provider.api_key)
    } else {
        request.bearer_auth(&provider.api_key)
    };
    tokio::time::timeout(Duration::from_secs(45), request.json(body).send())
        .await
        .map_err(|_| claude_error(StatusCode::GATEWAY_TIMEOUT, "等待 OpenAI 响应超时"))?
        .map_err(|_| {
            claude_error(
                StatusCode::BAD_GATEWAY,
                "无法连接 OpenAI 供应商，请检查接入地址和网络",
            )
        })
}
async fn read_openai(response: reqwest::Response) -> std::result::Result<Value, HttpError> {
    read_json(response)
        .await
        .map_err(|(status, _)| claude_error(status, "OpenAI 响应无效、过大或连接中断"))
}
pub(super) async fn models(
    State(runtime): State<Runtime>,
    Path((target, id)): Path<(String, String)>,
    headers: HeaderMap,
) -> std::result::Result<Json<Value>, HttpError> {
    let provider = authorize_claude(&runtime, &headers, &target, &id)?;
    let ids = reverse::model_ids(&provider);
    let list:Vec<_>=ids.iter().map(|id|json!({"id":if target=="claude_desktop"{reverse::model_alias(id)}else{id.clone()},"type":"model","display_name":id,"created_at":"2026-01-01T00:00:00Z"})).collect();
    Ok(Json(
        json!({"data":list,"has_more":false,"first_id":list.first().map(|v|v["id"].clone()),"last_id":list.last().map(|v|v["id"].clone())}),
    ))
}
pub(super) async fn count_tokens(
    State(runtime): State<Runtime>,
    Path((target, id)): Path<(String, String)>,
    headers: HeaderMap,
    Json(body): Json<Value>,
) -> std::result::Result<Response, HttpError> {
    let provider = authorize_claude(&runtime, &headers, &target, &id)?;
    let model = reverse::resolve_model(&provider, body["model"].as_str().unwrap_or(""))
        .map_err(|e| claude_error(StatusCode::BAD_REQUEST, e))?;
    let converted = reverse::request(&body, &model, false)
        .map_err(|e| claude_error(StatusCode::BAD_REQUEST, e))?;
    // No billed inference is sent by count_tokens. A byte-based conservative
    // estimate includes tool schemas and image payloads; it is not a tokenizer.
    let tokens = (converted.to_string().len().div_ceil(3) + 64) as u64;
    Ok((
        [("x-uni-switch-token-count", "estimated")],
        Json(json!({"input_tokens":tokens})),
    )
        .into_response())
}
pub(super) async fn messages(
    State(runtime): State<Runtime>,
    Path((target, id)): Path<(String, String)>,
    headers: HeaderMap,
    Json(body): Json<Value>,
) -> std::result::Result<Response, HttpError> {
    let provider = authorize_claude(&runtime, &headers, &target, &id)?;
    let client_model = body["model"]
        .as_str()
        .ok_or_else(|| claude_error(StatusCode::BAD_REQUEST, "请求缺少 model"))?
        .to_owned();
    let model = reverse::resolve_model(&provider, &client_model)
        .map_err(|e| claude_error(StatusCode::BAD_REQUEST, e))?;
    let streaming = body["stream"] == true;
    let converted = reverse::request(&body, &model, streaming)
        .map_err(|e| claude_error(StatusCode::BAD_REQUEST, e))?;
    let stops = body
        .get("stop_sequences")
        .filter(|v| v.as_array().is_none_or(|a| !a.is_empty()));
    if stops.is_some_and(|v| !v.as_array().is_some_and(|a| a.iter().all(Value::is_string))) {
        return Err(claude_error(
            StatusCode::BAD_REQUEST,
            "stop_sequences 必须是文本数组",
        ));
    }
    // Responses does not support stop sequences. Select Chat explicitly when
    // the client requires them; never silently discard the stopping contract.
    let mut chat = stops.is_some();
    let initial = if chat {
        let mut value = reverse::chat_request(&converted)
            .map_err(|e| claude_error(StatusCode::BAD_REQUEST, e))?;
        value["stop"] = stops.unwrap().clone();
        value
    } else {
        converted.clone()
    };
    let mut response = send(&runtime, &provider, &initial, chat).await?;
    if !chat && matches!(response.status().as_u16(), 404 | 405) {
        drop(response);
        let fallback = reverse::chat_request(&converted)
            .map_err(|e| claude_error(StatusCode::BAD_REQUEST, e))?;
        response = send(&runtime, &provider, &fallback, true).await?;
        chat = true;
    }
    if !response.status().is_success() {
        let status =
            StatusCode::from_u16(response.status().as_u16()).unwrap_or(StatusCode::BAD_GATEWAY);
        let data = read_openai(response).await.unwrap_or(Value::Null);
        let message = data
            .pointer("/error/message")
            .and_then(Value::as_str)
            .unwrap_or("OpenAI 供应商拒绝请求");
        return Err(claude_error(
            status,
            format!(
                "OpenAI HTTP {}：{}",
                status.as_u16(),
                redact(message, &provider.api_key)
            ),
        ));
    }
    if !streaming {
        let value = read_openai(response).await?;
        let message = reverse::response(&value, &client_model, chat)
            .map_err(|e| claude_error(StatusCode::BAD_GATEWAY, redact(&e, &provider.api_key)))?;
        return Ok(Json(message).into_response());
    }
    if !response
        .headers()
        .get(header::CONTENT_TYPE)
        .and_then(|h| h.to_str().ok())
        .is_some_and(|s| s.starts_with("text/event-stream"))
    {
        return Err(claude_error(
            StatusCode::BAD_GATEWAY,
            "OpenAI 供应商没有返回 SSE 流，请检查 API 协议",
        ));
    }
    let mut translator = reverse::Translator::new(&client_model);
    let mut decoder = stream::SseDecoder::default();
    // Cancellation drops the owned upstream stream along with the downstream.
    let output = async_stream::stream! {
        yield Ok::<_,Infallible>(sse(translator.started()));
        let mut chunks=response.bytes_stream();
        let mut failure=None;
        'read:loop {
            match tokio::time::timeout(Duration::from_secs(90),chunks.next()).await {
                Ok(Some(Ok(chunk)))=>match decoder.feed(&chunk) {
                    Ok(events)=>for event in events {
                        let result=if chat {translator.accept_chat(event)} else {translator.accept_responses(event)};
                        match result {Ok(events)=>for event in events {yield Ok(sse(event));},Err(e)=>{failure=Some(e);break 'read;}}
                        if translator.finished {break 'read;}
                    },
                    Err(e)=>{failure=Some(e);break;}
                },
                Ok(None)=>{
                    if chat && !translator.finished {
                        match translator.finish_chat(){Ok(events)=>for e in events {yield Ok(sse(e));},Err(e)=>failure=Some(e)}
                    } else if !translator.finished {failure=Some("OpenAI 流在完成事件前断开".into());}
                    break;
                }
                Ok(Some(Err(_)))=>{failure=Some("OpenAI 流式连接中断".into());break;}
                Err(_)=>{failure=Some("OpenAI 流式回复等待超时".into());break;}
            }
        }
        if let Some(message)=failure {
            yield Ok(sse(json!({"type":"error","error":{"type":"api_error","message":redact(&message,&provider.api_key)}})));
        }
    };
    Ok((
        [
            (header::CONTENT_TYPE, "text/event-stream"),
            (header::CACHE_CONTROL, "no-cache"),
        ],
        Body::from_stream(output),
    )
        .into_response())
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn normalizes_openai_endpoints() {
        assert_eq!(
            openai_endpoint("https://example.test/v1/", false),
            "https://example.test/v1/responses"
        );
        assert_eq!(
            openai_endpoint("https://example.test/openai", true),
            "https://example.test/openai/v1/chat/completions"
        );
        assert_eq!(
            openai_endpoint("https://example.test/v1/responses", true),
            "https://example.test/v1/chat/completions"
        );
        assert_eq!(
            openai_endpoint("https://example.test/v1/chat/completions", false),
            "https://example.test/v1/responses"
        );
    }
}
