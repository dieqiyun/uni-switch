use crate::error::{AppError, Result};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use std::{
    path::Path,
    time::{Duration, SystemTime, UNIX_EPOCH},
};

#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq)]
#[serde(rename_all = "snake_case")]
pub enum Endpoint {
    Messages,
    ChatCompletions,
    Responses,
}
#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq)]
#[serde(rename_all = "snake_case")]
pub enum Feature {
    Text,
    Image,
    Tools,
    Stream,
}
#[derive(Debug, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Observation {
    pub model: String,
    pub endpoint: Endpoint,
    pub feature: Feature,
    pub state: String,
    pub checked_at: u64,
    pub message: String,
}
fn payload(model: &str, endpoint: Endpoint, feature: Feature) -> Value {
    let prompt = if feature == Feature::Image {
        "Name the color of this image using only one English word."
    } else if feature == Feature::Tools {
        "Call qa_echo with marker QA_OK. Do not call any other tool."
    } else {
        "Reply with exactly QA_OK and nothing else."
    };
    let image = "iVBORw0KGgoAAAANSUhEUgAAACAAAAAgCAIAAAD8GO2jAAAAKElEQVR4nO3NsQ0AAAzCMP5/un0CNkuZ41wybXsHAAAAAAAAAAAAxR4yw/wuPL6QkAAAAABJRU5ErkJggg==";
    let mut content = if endpoint == Endpoint::Messages {
        json!([{"type":"text","text":prompt}])
    } else if endpoint == Endpoint::Responses {
        json!([{"type":"input_text","text":prompt}])
    } else {
        json!([{"type":"text","text":prompt}])
    };
    if feature == Feature::Image {
        let part = match endpoint {
            Endpoint::Messages => {
                json!({"type":"image","source":{"type":"base64","media_type":"image/png","data":image}})
            }
            Endpoint::Responses => {
                json!({"type":"input_image","image_url":format!("data:image/png;base64,{image}")})
            }
            Endpoint::ChatCompletions => {
                json!({"type":"image_url","image_url":{"url":format!("data:image/png;base64,{image}")}})
            }
        };
        content.as_array_mut().unwrap().push(part);
    }
    let mut body = json!({"model":model,"stream":feature == Feature::Stream});
    if endpoint == Endpoint::Responses {
        body["input"] = json!([{"role":"user","content":content}]);
        body["max_output_tokens"] = json!(256);
        body["store"] = json!(false);
    } else {
        body["messages"] = json!([{"role":"user","content":content}]);
        body["max_tokens"] = json!(256);
    }
    if feature == Feature::Tools {
        let schema = json!({"type":"object","properties":{"marker":{"type":"string"}},"required":["marker"],"additionalProperties":false});
        match endpoint {
            Endpoint::Messages => {
                body["tools"] = json!([{"name":"qa_echo","description":"Synthetic validation only; never executed","input_schema":schema}]);
                body["tool_choice"] = json!({"type":"tool","name":"qa_echo"});
            }
            Endpoint::Responses => {
                body["tools"] =
                    json!([{"type":"function","name":"qa_echo","parameters":schema,"strict":true}]);
                body["tool_choice"] = json!({"type":"function","name":"qa_echo"});
            }
            Endpoint::ChatCompletions => {
                body["tools"] =
                    json!([{"type":"function","function":{"name":"qa_echo","parameters":schema}}]);
                body["tool_choice"] = json!({"type":"function","function":{"name":"qa_echo"}});
            }
        }
    }
    body
}
fn validate_response(value: &Value, endpoint: Endpoint, feature: Feature) -> bool {
    if feature == Feature::Tools {
        let check = |name: Option<&str>, arguments: &Value| {
            name == Some("qa_echo") && arguments["marker"] == "QA_OK"
        };
        return match endpoint {
            Endpoint::Messages => value["content"].as_array().is_some_and(|parts| {
                parts.iter().any(|part| {
                    part["type"] == "tool_use" && check(part["name"].as_str(), &part["input"])
                })
            }),
            Endpoint::Responses => value["output"].as_array().is_some_and(|parts| {
                parts.iter().any(|part| {
                    part["type"] == "function_call"
                        && check(
                            part["name"].as_str(),
                            &serde_json::from_str::<Value>(
                                part["arguments"].as_str().unwrap_or(""),
                            )
                            .unwrap_or(Value::Null),
                        )
                })
            }),
            Endpoint::ChatCompletions => value["choices"][0]["message"]["tool_calls"]
                .as_array()
                .is_some_and(|calls| {
                    calls.iter().any(|call| {
                        check(
                            call["function"]["name"].as_str(),
                            &serde_json::from_str::<Value>(
                                call["function"]["arguments"].as_str().unwrap_or(""),
                            )
                            .unwrap_or(Value::Null),
                        )
                    })
                }),
        };
    }
    let text = match endpoint {
        Endpoint::Messages => value["content"]
            .as_array()
            .map(|parts| {
                parts
                    .iter()
                    .filter_map(|p| p["text"].as_str())
                    .collect::<String>()
            })
            .unwrap_or_default(),
        Endpoint::Responses => value["output"]
            .as_array()
            .map(|parts| {
                parts
                    .iter()
                    .filter_map(|p| p["content"].as_array())
                    .flatten()
                    .filter_map(|p| p["text"].as_str())
                    .collect::<String>()
            })
            .unwrap_or_default(),
        Endpoint::ChatCompletions => value["choices"][0]["message"]["content"]
            .as_str()
            .unwrap_or("")
            .into(),
    };
    if feature == Feature::Image {
        text.trim()
            .trim_end_matches('.')
            .eq_ignore_ascii_case("red")
    } else {
        text.trim() == "QA_OK"
    }
}
fn validate_stream(bytes: &[u8], endpoint: Endpoint) -> bool {
    let mut decoder = crate::bridge::stream::SseDecoder::default();
    let Ok(events) = decoder.feed(bytes) else {
        return false;
    };
    let mut reply = String::new();
    let mut completed = false;
    let mut done = false;
    let mut failed = false;
    for value in events {
        if value.get("error").is_some()
            || matches!(
                value["type"].as_str(),
                Some("error" | "response.failed" | "response.incomplete")
            )
        {
            failed = true;
        }
        match endpoint {
            Endpoint::Messages => {
                if value["type"] == "message_stop" {
                    completed = true;
                }
                if value["type"] == "content_block_delta" && value["delta"]["type"] == "text_delta"
                {
                    reply.push_str(value["delta"]["text"].as_str().unwrap_or(""));
                }
            }
            Endpoint::Responses => {
                if value["type"] == "response.completed" {
                    completed = true;
                }
                if value["type"] == "response.output_text.delta" {
                    reply.push_str(value["delta"].as_str().unwrap_or(""));
                }
            }
            Endpoint::ChatCompletions => {
                if value["type"] == "uni_switch_done" {
                    done = true;
                }
                if value["choices"][0]["finish_reason"] == "stop" {
                    completed = true;
                }
                reply.push_str(
                    value["choices"][0]["delta"]["content"]
                        .as_str()
                        .unwrap_or(""),
                );
            }
        }
    }
    completed
        && !failed
        && reply.trim() == "QA_OK"
        && (endpoint != Endpoint::ChatCompletions || done)
}
pub fn require_consent(consent: bool) -> Result<()> {
    if !consent {
        return Err(AppError::new(
            "verification_consent",
            "验证会发送少量推理请求并可能计费，请先确认",
        ));
    }
    Ok(())
}
pub async fn verify(
    base: &str,
    key: &str,
    auth: &str,
    model: &str,
    endpoint: Endpoint,
    feature: Feature,
    consent: bool,
) -> Result<Observation> {
    require_consent(consent)?;
    if model.is_empty()
        || model.len() > 256
        || model.chars().any(char::is_control)
        || !matches!(auth, "bearer" | "x-api-key")
    {
        return Err(AppError::new(
            "verification_input",
            "验证的模型或认证方式无效",
        ));
    }
    let mut url = crate::types::validate_url(base)?;
    let root = url.path().trim_end_matches('/');
    let root = ["/chat/completions", "/responses", "/messages"]
        .iter()
        .find_map(|suffix| root.strip_suffix(suffix))
        .unwrap_or(root);
    let resource = match endpoint {
        Endpoint::Messages => "messages",
        Endpoint::ChatCompletions => "chat/completions",
        Endpoint::Responses => "responses",
    };
    let path = if root.ends_with("/v1") {
        format!("{root}/{resource}")
    } else {
        format!("{root}/v1/{resource}")
    };
    url.set_path(&path);
    let client = reqwest::Client::builder()
        .redirect(reqwest::redirect::Policy::none())
        .timeout(Duration::from_secs(35))
        .build()
        .map_err(|_| AppError::new("verification_network", "无法创建验证连接"))?;
    let mut request = client
        .post(url)
        .header("anthropic-version", "2023-06-01")
        .json(&payload(model, endpoint, feature));
    request = if auth == "bearer" {
        request.bearer_auth(key)
    } else {
        request.header("x-api-key", key)
    };
    let checked_at = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs();
    let mut observation = Observation {
        model: model.into(),
        endpoint,
        feature,
        state: "unknown".into(),
        checked_at,
        message: "验证未完成，不能据此认定不支持".into(),
    };
    let response = async {
        let mut response = request
            .send()
            .await
            .map_err(|_| "网络连接失败或超时；能力仍待确认".to_owned())?;
        let status = response.status().as_u16();
        if !(200..300).contains(&status) {
            return Err(format!(
                "接口返回 HTTP {status}；能力仍待确认，不会永久禁用"
            ));
        }
        if feature == Feature::Stream
            && !response
                .headers()
                .get(reqwest::header::CONTENT_TYPE)
                .and_then(|value| value.to_str().ok())
                .is_some_and(|value| value.starts_with("text/event-stream"))
        {
            return Err("接口没有返回 SSE 流，流式能力仍待确认".into());
        }
        let mut bytes = Vec::new();
        while let Some(chunk) = response
            .chunk()
            .await
            .map_err(|_| "验证响应中断；能力仍待确认".to_owned())?
        {
            if bytes.len() + chunk.len() > 256 * 1024 {
                return Err("验证响应超过大小限制；能力仍待确认".into());
            }
            bytes.extend_from_slice(&chunk);
            if feature == Feature::Stream && validate_stream(&bytes, endpoint) {
                return Ok(true);
            }
        }
        Ok(if feature == Feature::Stream {
            validate_stream(&bytes, endpoint)
        } else {
            serde_json::from_slice::<Value>(&bytes)
                .ok()
                .is_some_and(|value| validate_response(&value, endpoint, feature))
        })
    };
    match tokio::time::timeout(Duration::from_secs(40), response).await {
        Ok(Ok(true)) => {
            observation.state = "verified".into();
            observation.message =
                "本次合成请求验证通过；仅代表该模型、接口及本次条件，不自动覆盖手动设置".into();
        }
        Ok(Ok(false)) => {
            observation.message = "HTTP 成功，但回复未满足验证要求；能力仍待确认".into()
        }
        Ok(Err(message)) => observation.message = message,
        Err(_) => observation.message = "验证超时；能力仍待确认".into(),
    }
    Ok(observation)
}
pub fn save(directory: &Path, base: &str, result: &Observation) -> Result<()> {
    let digest = format!(
        "{:x}",
        Sha256::digest(format!(
            "{base}\n{}\n{:?}\n{:?}",
            result.model, result.endpoint, result.feature
        ))
    );
    let path = directory.join("model-verifications");
    crate::writer::private_directory(&path)?;
    let data = serde_json::to_string(result)
        .map_err(|_| AppError::new("verification_save", "验证结果无法保存"))?;
    crate::writer::write(&path.join(format!("{digest}.json")), Some(&data))
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn success_requires_semantic_evidence() {
        assert!(!validate_response(
            &json!({}),
            Endpoint::Messages,
            Feature::Text
        ));
        assert!(validate_response(
            &json!({"content":[{"type":"text","text":"QA_OK"}]}),
            Endpoint::Messages,
            Feature::Text
        ));
        assert!(!validate_response(
            &json!({"content":[{"type":"text","text":"I cannot see images"}]}),
            Endpoint::Messages,
            Feature::Image
        ));
        assert!(validate_response(
            &json!({"content":[{"type":"text","text":"Red"}]}),
            Endpoint::Messages,
            Feature::Image
        ));
        assert!(validate_response(
            &json!({"choices":[{"message":{"tool_calls":[{"function":{"name":"qa_echo","arguments":"{\"marker\":\"QA_OK\"}"}}]}}]}),
            Endpoint::ChatCompletions,
            Feature::Tools
        ));
        assert!(!validate_response(
            &json!({"content":[{"type":"tool_use","name":"dangerous","input":{"marker":"QA_OK"}}]}),
            Endpoint::Messages,
            Feature::Tools
        ));
        let incomplete = b"data: {\"type\":\"response.output_text.delta\",\"delta\":\"QA_OK\"}\n\n";
        assert!(!validate_stream(incomplete, Endpoint::Responses));
        let complete = [
            incomplete.as_slice(),
            b"data: {\"type\":\"response.completed\"}\n\n",
        ]
        .concat();
        assert!(validate_stream(&complete, Endpoint::Responses));
        let failed = [
            complete.as_slice(),
            b"data: {\"type\":\"error\",\"error\":{}}\n\n",
        ]
        .concat();
        assert!(!validate_stream(&failed, Endpoint::Responses));
    }

    #[tokio::test]
    async fn verification_does_not_retry_errors_or_leak_keys_and_saves_observations_only() {
        use std::io::{Read, Write};
        let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
        let base = format!("http://{}/v1", listener.local_addr().unwrap());
        let worker = std::thread::spawn(move || {
            let (mut stream, _) = listener.accept().unwrap();
            let mut buffer = [0; 8192];
            let size = stream.read(&mut buffer).unwrap();
            let request = String::from_utf8_lossy(&buffer[..size]).into_owned();
            let body = "{\"error\":{\"message\":\"synthetic-private-key\"}}";
            write!(stream, "HTTP/1.1 500 Internal Server Error\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}", body.len()).unwrap();
            request
        });
        let result = verify(
            &base,
            "synthetic-private-key",
            "bearer",
            "test-model",
            Endpoint::Responses,
            Feature::Text,
            true,
        )
        .await
        .unwrap();
        assert_eq!(result.state, "unknown");
        assert!(!result.message.contains("synthetic-private-key"));
        assert!(worker.join().unwrap().starts_with("POST /v1/responses "));
        let temp = tempfile::tempdir().unwrap();
        save(temp.path(), &base, &result).unwrap();
        assert!(!temp.path().join("uni-switch.db").exists());
        let path = std::fs::read_dir(temp.path().join("model-verifications"))
            .unwrap()
            .next()
            .unwrap()
            .unwrap()
            .path();
        assert!(!std::fs::read_to_string(path)
            .unwrap()
            .contains("synthetic-private-key"));
    }
    #[tokio::test]
    async fn consent_is_required_before_any_connection() {
        let result = verify(
            "not-a-url",
            "synthetic-key",
            "bearer",
            "model",
            Endpoint::Responses,
            Feature::Text,
            false,
        )
        .await
        .unwrap_err();
        assert_eq!(result.code, "verification_consent");
    }
    #[tokio::test]
    async fn success_validates_all_protocols_and_bounded_synthetic_payloads() {
        use axum::{extract::Request, routing::post, Router};
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let base = format!("http://{}", listener.local_addr().unwrap());
        let router = Router::new().route("/v1/{*endpoint}", post(|request: Request| async move {
            let path = request.uri().path().to_owned();
            let bytes = axum::body::to_bytes(request.into_body(), 64 * 1024).await.unwrap();
            let body:Value = serde_json::from_slice(&bytes).unwrap();
            assert_eq!(body["model"], "synthetic-model");
            assert_eq!(body[if path.ends_with("responses") {"max_output_tokens"} else {"max_tokens"}], 256);
            if body["stream"] == true {
                let stream = if path.ends_with("responses") {
                    "data: {\"type\":\"response.output_text.delta\",\"delta\":\"QA_OK\"}\n\ndata: {\"type\":\"response.completed\"}\n\n"
                } else if path.ends_with("messages") {
                    "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"QA_OK\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n"
                } else {
                    "data: {\"choices\":[{\"delta\":{\"content\":\"QA_OK\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
                };
                return ([("content-type","text/event-stream")],stream.to_owned());
            }
            let image = bytes.windows(9).any(|value| value == b"image/png");
            let tool = body.get("tools").is_some();
            let text = if image {"red"} else {"QA_OK"};
            let value = if path.ends_with("messages") {
                json!({"content":if tool {json!([{"type":"tool_use","name":"qa_echo","input":{"marker":"QA_OK"}}])} else {json!([{"type":"text","text":text}])}})
            } else if path.ends_with("responses") {
                json!({"output":if tool {json!([{"type":"function_call","name":"qa_echo","arguments":"{\"marker\":\"QA_OK\"}"}])} else {json!([{"type":"message","content":[{"type":"output_text","text":text}]}])}})
            } else {
                json!({"choices":[{"message":if tool {json!({"tool_calls":[{"function":{"name":"qa_echo","arguments":"{\"marker\":\"QA_OK\"}"}}]})} else {json!({"content":text})}}]})
            };
            ([("content-type","application/json")],value.to_string())
        }));
        let server = tokio::spawn(async move { axum::serve(listener, router).await.unwrap() });
        for endpoint in [
            Endpoint::Messages,
            Endpoint::ChatCompletions,
            Endpoint::Responses,
        ] {
            for feature in [
                Feature::Text,
                Feature::Image,
                Feature::Tools,
                Feature::Stream,
            ] {
                let result = verify(
                    &base,
                    "synthetic-key",
                    "bearer",
                    "synthetic-model",
                    endpoint,
                    feature,
                    true,
                )
                .await
                .unwrap();
                assert_eq!(
                    result.state, "verified",
                    "{endpoint:?} {feature:?}: {}",
                    result.message
                );
            }
        }
        server.abort();
    }
}
