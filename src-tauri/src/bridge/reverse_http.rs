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
    crate::api_endpoint::openai(base, chat)
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
    let mut list: Vec<Value> = Vec::new();
    for id in &ids {
        let fallback = crate::types::ProviderModel {
            id: id.clone(),
            ..Default::default()
        };
        let configured = provider
            .summary
            .codex_options
            .models
            .iter()
            .find(|m| &m.id == id)
            .unwrap_or(&fallback);
        let caps = crate::model_capabilities::resolve(configured);
        let (name, supports_1m) = crate::claude_models::split_context_marker(id);
        let route_id = if target == "claude_desktop" {
            reverse::model_alias(id)
        } else {
            id.clone()
        };
        if let Some(existing) = list.iter_mut().find(|m| m["id"] == route_id) {
            if supports_1m {
                existing["supports1m"] = json!(true);
            }
            continue;
        }
        let mut model = json!({"id":route_id,"type":"model","display_name":name,"created_at":"2026-01-01T00:00:00Z","capabilities":{"image_input":{"supported":caps.image_input}}});
        if supports_1m {
            model["supports1m"] = json!(true);
        }
        list.push(model);
    }
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
    let fallback = crate::types::ProviderModel {
        id: model.clone(),
        ..Default::default()
    };
    let configured = provider
        .summary
        .codex_options
        .models
        .iter()
        .find(|m| crate::claude_models::split_context_marker(&m.id).0 == model)
        .unwrap_or(&fallback);
    let streaming = body["stream"] == true;
    let converted = reverse::request_with_model(&body, &model, streaming, configured)
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
    let profile = crate::model_capabilities::profile(configured);
    let mut chat = stops.is_some()
        || profile.endpoints.responses == Some(false)
            && profile.endpoints.chat_completions == Some(true);
    if chat && profile.endpoints.chat_completions == Some(false)
        || !chat && profile.endpoints.responses == Some(false)
    {
        return Err(claude_error(
            StatusCode::BAD_REQUEST,
            "此模型未提供当前请求需要的 OpenAI 接口，请检查模型接口声明",
        ));
    }
    let initial = if chat {
        let mut value = reverse::chat_request_with_model(&converted, configured)
            .map_err(|e| claude_error(StatusCode::BAD_REQUEST, e))?;
        if let Some(stops) = stops {
            value["stop"] = stops.clone();
        }
        value
    } else {
        converted.clone()
    };
    let mut response = send(&runtime, &provider, &initial, chat).await?;
    if !chat
        && profile.endpoints.chat_completions != Some(false)
        && matches!(response.status().as_u16(), 404 | 405)
    {
        drop(response);
        let fallback = reverse::chat_request_with_model(&converted, configured)
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
                    if let Err(error) = decoder.finish() {
                        failure = Some(error);
                    } else if chat && !translator.finished {
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
    fn usage_audit_custom_openai_prefix_is_authoritative() {
        for base in [
            "https://example.test/api/paas/v4",
            "https://example.test/backend-api/codex",
            "https://example.test/gateway/responses",
        ] {
            let expected = if let Some(root) = base.strip_suffix("/responses") {
                format!("{root}/chat/completions")
            } else {
                format!("{base}/chat/completions")
            };
            assert_eq!(openai_endpoint(base, true), expected);
        }
    }

    #[test]
    fn normalizes_openai_endpoints() {
        assert_eq!(
            openai_endpoint("https://example.test/v1/", false),
            "https://example.test/v1/responses"
        );
        assert_eq!(
            openai_endpoint("https://example.test/openai", true),
            "https://example.test/openai/chat/completions"
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
    #[tokio::test]
    async fn system_messages_work_for_both_endpoints_streams_and_token_counts() {
        use crate::types::{
            ClaudeProtocol, CodexOptions, EndpointSupport, Family, ProviderInput, ProviderModel,
        };
        for target in [Target::ClaudeCli, Target::ClaudeDesktop] {
            for (chat, prefix) in [
                (false, "/v1"),
                (true, "/v1"),
                (false, "/backend-api/codex"),
                (true, "/api/paas/v4"),
            ] {
                let captured = Arc::new(Mutex::new(Vec::<Value>::new()));
                let captured_upstream = captured.clone();
                let upstream = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
                let base = format!("http://{}{prefix}", upstream.local_addr().unwrap());
                let endpoint = format!(
                    "{prefix}/{}",
                    if chat {
                        "chat/completions"
                    } else {
                        "responses"
                    }
                );
                let cr_lines = prefix != "/v1";
                let router = Router::new().route(&endpoint, post(move |Json(body): Json<Value>| {
                    let captured = captured_upstream.clone();
                    async move {
                        captured.lock().unwrap().push(body.clone());
                        let text = "QA_SYSTEM_OK";
                        if body["stream"] == true {
                            let wire = if chat && body.to_string().contains("QA_TRUNCATED_STREAM") {
                                format!("data: {}\n\ndata: {{\"unfinished\":", json!({"choices":[{"delta":{"content":text},"finish_reason":"stop"}]}))
                            } else if chat {
                                format!("data: {}\n\ndata: [DONE]\n\n", json!({"choices":[{"delta":{"content":text},"finish_reason":"stop"}]}))
                            } else {
                                format!("data: {}\n\ndata: {}\n\n",
                                    json!({"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":text}),
                                    json!({"type":"response.completed","response":{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":text}]}]}}))
                            };
                            let wire = if cr_lines { wire.replace('\n', "\r") } else { wire };
                            ([(header::CONTENT_TYPE, "text/event-stream")], wire).into_response()
                        } else if chat {
                            Json(json!({"choices":[{"message":{"content":text},"finish_reason":"stop"}]})).into_response()
                        } else {
                            Json(json!({"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":text}]}]})).into_response()
                        }
                    }
                }));
                let server =
                    tokio::spawn(async move { axum::serve(upstream, router).await.unwrap() });
                let temp = tempfile::tempdir().unwrap();
                let mut store = Store::open(temp.path().join("data")).unwrap();
                store
                    .set_directory(
                        target,
                        temp.path().join("isolated-client").to_string_lossy().into(),
                    )
                    .unwrap();
                let token = "b".repeat(64);
                store.set_bridge_route(Route {
                    port: 19873,
                    token: token.clone(),
                });
                let mut model = ProviderModel {
                    id: "gpt-5.4".into(),
                    ..Default::default()
                };
                model.profile.endpoints = EndpointSupport {
                    responses: Some(!chat),
                    chat_completions: Some(chat),
                    ..Default::default()
                };
                let provider = store
                    .save(ProviderInput {
                        id: None,
                        family: Family::Claude,
                        name: "Synthetic system QA".into(),
                        base_url: base,
                        api_key: Some("synthetic-system-key".into()),
                        balance_access_token: None,
                        model: model.id.clone(),
                        auth_mode: "bearer".into(),
                        reasoning_effort: None,
                        codex_options: CodexOptions {
                            claude_protocol: ClaudeProtocol::Openai,
                            models: vec![model],
                            ..Default::default()
                        },
                    })
                    .unwrap();
                store.apply(target, &provider.id).unwrap();
                let runtime = Runtime {
                    store: Arc::new(Mutex::new(store)),
                    token: token.clone(),
                    client: reqwest::Client::builder().no_proxy().build().unwrap(),
                };
                let mut headers = HeaderMap::new();
                headers.insert("x-api-key", token.parse().unwrap());
                let path = (target.id().to_owned(), provider.id.clone());
                let source = json!({"model":"gpt-5.4","system":"Initial instructions","max_tokens":256,"thinking":{"type":"adaptive"},"messages":[
                    {"role":"user","content":"Previous turn"},
                    {"role":"assistant","content":"Previous reply"},
                    {"role":"user","content":"Current turn"},
                    {"role":"system","content":"Current instructions","clear_at":"next_user_message","output_config":{"effort":"low"}}
                ]});
                let count = count_tokens(
                    State(runtime.clone()),
                    Path(path.clone()),
                    headers.clone(),
                    Json(source.clone()),
                )
                .await
                .unwrap();
                assert_eq!(count.status(), StatusCode::OK);
                assert_eq!(count.headers()["x-uni-switch-token-count"], "estimated");
                let count: Value = serde_json::from_slice(
                    &axum::body::to_bytes(count.into_body(), 1024).await.unwrap(),
                )
                .unwrap();
                assert!(count["input_tokens"].as_u64().unwrap() > 0);
                assert!(
                    captured.lock().unwrap().is_empty(),
                    "Counting must not call inference"
                );
                for streaming in [false, true] {
                    let mut body = source.clone();
                    body["stream"] = json!(streaming);
                    let reply = messages(
                        State(runtime.clone()),
                        Path(path.clone()),
                        headers.clone(),
                        Json(body),
                    )
                    .await
                    .unwrap();
                    assert_eq!(reply.status(), StatusCode::OK);
                    let reply = String::from_utf8(
                        axum::body::to_bytes(reply.into_body(), 64 * 1024)
                            .await
                            .unwrap()
                            .to_vec(),
                    )
                    .unwrap();
                    assert!(reply.contains("QA_SYSTEM_OK"), "{reply}");
                    assert!(
                        reply.contains(if streaming {
                            "message_stop"
                        } else {
                            "end_turn"
                        }),
                        "{reply}"
                    );
                }
                let requests = captured.lock().unwrap().clone();
                assert_eq!(requests.len(), 2);
                for request in requests {
                    if chat {
                        assert_eq!(request["messages"][0]["role"], "system");
                        assert_eq!(request["messages"][4]["role"], "system");
                        assert_eq!(
                            request["messages"][4]["content"][0]["text"],
                            "Current instructions"
                        );
                        assert_eq!(request["reasoning_effort"], "low");
                    } else {
                        assert_eq!(request["instructions"], "Initial instructions");
                        assert_eq!(request["input"][3]["role"], "system");
                        assert_eq!(
                            request["input"][3]["content"][0]["text"],
                            "Current instructions"
                        );
                        assert_eq!(request["reasoning"]["effort"], "low");
                    }
                }
                for invalid in [
                    json!({"role":"synthetic-secret-role","content":"synthetic-secret-text"}),
                    json!({"role":"system","content":[{"type":"image","source":{"type":"url","url":"https://example.test/synthetic-secret"}}]}),
                ] {
                    let mut body = source.clone();
                    body["messages"].as_array_mut().unwrap().push(invalid);
                    let (status, Json(error)) = messages(
                        State(runtime.clone()),
                        Path(path.clone()),
                        headers.clone(),
                        Json(body.clone()),
                    )
                    .await
                    .unwrap_err();
                    assert_eq!(status, StatusCode::BAD_REQUEST);
                    assert_eq!(error["error"]["type"], "invalid_request_error");
                    assert!(!error.to_string().contains("synthetic-secret"));
                    assert_eq!(
                        count_tokens(
                            State(runtime.clone()),
                            Path(path.clone()),
                            headers.clone(),
                            Json(body)
                        )
                        .await
                        .unwrap_err()
                        .0,
                        StatusCode::BAD_REQUEST
                    );
                }
                assert_eq!(
                    captured.lock().unwrap().len(),
                    2,
                    "Invalid inputs must not reach upstream"
                );
                if chat {
                    let body = json!({"model":"gpt-5.4","stream":true,"max_tokens":256,"messages":[{"role":"user","content":"QA_TRUNCATED_STREAM"}]});
                    let response = messages(
                        State(runtime.clone()),
                        Path(path.clone()),
                        headers.clone(),
                        Json(body),
                    )
                    .await
                    .unwrap();
                    let wire = String::from_utf8(
                        axum::body::to_bytes(response.into_body(), 64 * 1024)
                            .await
                            .unwrap()
                            .to_vec(),
                    )
                    .unwrap();
                    assert!(wire.contains("event: error"), "{wire}");
                    assert!(
                        !wire.contains("event: message_stop"),
                        "truncated SSE must not be reported as a successful message: {wire}"
                    );
                }
                server.abort();
            }
        }
    }

    #[tokio::test]
    async fn declared_endpoints_avoid_responses_and_errors_do_not_invent_fallbacks() {
        use crate::types::{
            ClaudeProtocol, CodexOptions, EndpointSupport, Family, ProviderInput, ProviderModel,
        };
        for (support, status, expected_status, expected_paths) in [
            (
                EndpointSupport {
                    responses: Some(false),
                    chat_completions: Some(true),
                    ..Default::default()
                },
                500,
                200,
                vec!["/v1/chat/completions"],
            ),
            (EndpointSupport::default(), 500, 500, vec!["/v1/responses"]),
            (
                EndpointSupport {
                    chat_completions: Some(false),
                    ..Default::default()
                },
                404,
                404,
                vec!["/v1/responses"],
            ),
            (
                EndpointSupport::default(),
                404,
                200,
                vec!["/v1/responses", "/v1/chat/completions"],
            ),
        ] {
            let paths = Arc::new(Mutex::new(Vec::<String>::new()));
            let captured = paths.clone();
            let upstream = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
            let base = format!("http://{}/v1", upstream.local_addr().unwrap());
            let router = Router::new().route("/v1/{*resource}", post(move |request: axum::extract::Request| {
                let captured = captured.clone();
                async move {
                    let path = request.uri().path().to_owned(); captured.lock().unwrap().push(path.clone());
                    if path.ends_with("responses") {
                        (StatusCode::from_u16(status).unwrap(),Json(json!({"error":{"message":"not implemented"}})))
                    } else {
                        (StatusCode::OK,Json(json!({"choices":[{"message":{"content":"QA_OK"},"finish_reason":"stop"}]})))
                    }
                }
            }));
            let server = tokio::spawn(async move { axum::serve(upstream, router).await.unwrap() });
            let temp = tempfile::tempdir().unwrap();
            let mut store = Store::open(temp.path().join("data")).unwrap();
            store
                .set_directory(
                    Target::ClaudeCli,
                    temp.path().join("isolated-claude").to_string_lossy().into(),
                )
                .unwrap();
            let token = "a".repeat(64);
            store.set_bridge_route(Route {
                port: 19872,
                token: token.clone(),
            });
            let mut model = ProviderModel {
                id: "gpt-4.1".into(),
                ..Default::default()
            };
            model.profile.endpoints = support;
            let provider = store
                .save(ProviderInput {
                    id: None,
                    family: Family::Claude,
                    name: "Synthetic endpoint QA".into(),
                    base_url: base,
                    api_key: Some("synthetic-key".into()),
                    balance_access_token: None,
                    model: model.id.clone(),
                    auth_mode: "bearer".into(),
                    reasoning_effort: None,
                    codex_options: CodexOptions {
                        claude_protocol: ClaudeProtocol::Openai,
                        models: vec![model],
                        ..Default::default()
                    },
                })
                .unwrap();
            store.apply(Target::ClaudeCli, &provider.id).unwrap();
            let runtime = Runtime {
                store: Arc::new(Mutex::new(store)),
                token: token.clone(),
                client: reqwest::Client::builder().no_proxy().build().unwrap(),
            };
            let mut headers = HeaderMap::new();
            headers.insert(
                header::AUTHORIZATION,
                format!("Bearer {token}").parse().unwrap(),
            );
            let result = messages(State(runtime),Path(("claude_cli".into(),provider.id)),headers,Json(json!({"model":"gpt-4.1","messages":[{"role":"user","content":"QA"}],"max_tokens":256}))).await;
            let actual_status = match result {
                Ok(response) => response.status().as_u16(),
                Err((status, _)) => status.as_u16(),
            };
            assert_eq!(actual_status, expected_status);
            assert_eq!(*paths.lock().unwrap(), expected_paths);
            server.abort();
        }
    }
}

#[cfg(test)]
mod model_routing_tests {
    use super::*;
    use crate::types::{
        ClaudeProtocol, CodexOptions, EndpointSupport, Family, ProviderInput, ProviderModel,
    };

    #[tokio::test]
    async fn generated_profile_routes_reach_the_exact_upstream_model_for_both_openai_formats() {
        for chat in [false, true] {
            let captured = Arc::new(Mutex::new(Vec::<Value>::new()));
            let upstream_capture = captured.clone();
            let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
            let base = format!("http://{}/v1", listener.local_addr().unwrap());
            let router = Router::new().route(if chat { "/v1/chat/completions" } else { "/v1/responses" }, post(move |headers: HeaderMap, Json(body): Json<Value>| {
                let captured = upstream_capture.clone();
                async move {
                    assert_eq!(headers[header::AUTHORIZATION], "Bearer synthetic-model-key");
                    assert!(!body["model"].as_str().unwrap().contains("-uni-"));
                    captured.lock().unwrap().push(body.clone());
                    if body["stream"] == true {
                        let wire = if chat {
                            format!("data: {}\n\ndata: [DONE]\n\n", json!({"choices":[{"delta":{"content":"MODEL_OK"},"finish_reason":"stop"}]}))
                        } else {
                            format!("data: {}\n\ndata: {}\n\n", json!({"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"MODEL_OK"}), json!({"type":"response.completed","response":{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"MODEL_OK"}]}]}}))
                        };
                        ([(header::CONTENT_TYPE, "text/event-stream")], wire).into_response()
                    } else if chat { Json(json!({"choices":[{"message":{"content":"MODEL_OK"},"finish_reason":"stop"}]})).into_response() }
                    else { Json(json!({"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"MODEL_OK"}]}]})).into_response() }
                }
            }));
            let upstream =
                tokio::spawn(async move { axum::serve(listener, router).await.unwrap() });
            let temp = tempfile::tempdir().unwrap();
            let mut store = Store::open(temp.path().join("data")).unwrap();
            let directory = temp.path().join("isolated-desktop");
            store
                .set_directory(Target::ClaudeDesktop, directory.to_string_lossy().into())
                .unwrap();
            let token = "b".repeat(64);
            store.set_bridge_route(Route {
                port: 19872,
                token: token.clone(),
            });
            let ids = [
                "claude-sonnet-5-5",
                "anthropic/claude-opus-5-5",
                "claude-haiku-4-5",
                "gpt-4.1",
            ];
            let configured_models = ids
                .iter()
                .map(|id| {
                    let mut model = ProviderModel {
                        id: (*id).into(),
                        enabled: true,
                        ..Default::default()
                    };
                    model.capability_overrides.image_input = Some(false);
                    model.capabilities.image_input = Some(false);
                    model.profile.endpoints = EndpointSupport {
                        responses: Some(!chat),
                        chat_completions: Some(chat),
                        ..Default::default()
                    };
                    model
                })
                .collect();
            let provider = store
                .save(ProviderInput {
                    id: None,
                    family: Family::Claude,
                    name: "Synthetic model routes".into(),
                    base_url: base,
                    api_key: Some("synthetic-model-key".into()),
                    balance_access_token: None,
                    model: ids[0].into(),
                    auth_mode: "bearer".into(),
                    reasoning_effort: None,
                    codex_options: CodexOptions {
                        claude_protocol: ClaudeProtocol::Openai,
                        models: configured_models,
                        ..Default::default()
                    },
                })
                .unwrap();
            store.apply(Target::ClaudeDesktop, &provider.id).unwrap();
            let profile: Value = serde_json::from_str(
                &std::fs::read_to_string(directory.join(format!(
                    "Claude-3p/configLibrary/{}.json",
                    crate::adapters::PROFILE_UUID
                )))
                .unwrap(),
            )
            .unwrap();
            let runtime = Runtime {
                store: Arc::new(Mutex::new(store)),
                token: token.clone(),
                client: reqwest::Client::builder().no_proxy().build().unwrap(),
            };
            let mut headers = HeaderMap::new();
            headers.insert("x-api-key", token.parse().unwrap());
            let route = ("claude_desktop".to_owned(), provider.id.clone());
            let Json(catalog) =
                models(State(runtime.clone()), Path(route.clone()), headers.clone())
                    .await
                    .unwrap();
            for (index, expected) in ids.iter().enumerate() {
                let requested = profile["inferenceModels"][index]["name"].as_str().unwrap();
                assert_eq!(catalog["data"][index]["id"], requested);
                assert_eq!(
                    catalog["data"][index]["capabilities"]["image_input"]["supported"],
                    true
                );
                for streaming in [false, true] {
                    let reply = messages(State(runtime.clone()), Path(route.clone()), headers.clone(), Json(json!({"model":requested,"stream":streaming,"max_tokens":256,"messages":[{"role":"user","content":[{"type":"text","text":"synthetic model QA"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]}]}))).await.unwrap();
                    let text = String::from_utf8(
                        axum::body::to_bytes(reply.into_body(), 64 * 1024)
                            .await
                            .unwrap()
                            .to_vec(),
                    )
                    .unwrap();
                    assert!(text.contains("MODEL_OK"), "{text}");
                    assert!(text.contains(requested));
                    let captured = captured.lock().unwrap();
                    let body = captured.last().unwrap();
                    assert_eq!(body["model"], *expected);
                    assert!(body.to_string().contains("data:image/png;base64,AAAA"));
                }
            }
            let legacy = reverse::legacy_model_alias(ids[1]);
            for (requested, expected) in [
                (legacy.as_str(), ids[1]),
                ("claude-haiku-4-5-20251001 [1M]", ids[2]),
                ("claude-fable-5", ids[1]),
            ] {
                let body = json!({"model":requested,"max_tokens":256,"messages":[{"role":"user","content":"synthetic role QA"}]});
                count_tokens(
                    State(runtime.clone()),
                    Path(route.clone()),
                    headers.clone(),
                    Json(body.clone()),
                )
                .await
                .unwrap();
                let reply = messages(
                    State(runtime.clone()),
                    Path(route.clone()),
                    headers.clone(),
                    Json(body),
                )
                .await
                .unwrap();
                assert_eq!(reply.status(), StatusCode::OK);
                assert_eq!(captured.lock().unwrap().last().unwrap()["model"], expected);
            }
            let sent = captured.lock().unwrap().len();
            for requested in [
                "claude-unknown-5",
                "claude-sonnet-4-6-uni-missing",
                "missing",
            ] {
                let result = messages(State(runtime.clone()), Path(route.clone()), headers.clone(), Json(json!({"model":requested,"max_tokens":256,"messages":[{"role":"user","content":"synthetic invalid QA"}]}))).await;
                assert_eq!(result.unwrap_err().0, StatusCode::BAD_REQUEST);
            }
            assert_eq!(captured.lock().unwrap().len(), sent);
            upstream.abort();
        }
    }
}
