use super::*;

#[test]
fn new_claude_and_deepseek_thinking_use_declared_formats_not_budgets() {
    for effort in ["minimal", "medium", "xhigh", "ultra"] {
        let source = json!({"model":"claude-haiku-5-5","input":"Hi","reasoning":{"effort":effort},"max_output_tokens":256,"temperature":1,"top_p":0.8});
        let converted = convert::request(&source, false).unwrap();
        assert_eq!(converted.body["thinking"]["type"], "adaptive");
        assert!(converted.body["thinking"].get("budget_tokens").is_none());
        assert!(converted.body.get("temperature").is_none());
        assert!(converted.body.get("top_p").is_none());
        assert_eq!(
            converted.body["output_config"]["effort"],
            match effort {
                "minimal" => "low",
                "ultra" => "max",
                other => other,
            }
        );
    }
    let source = json!({"model":"deepseek-v4.1-flash","input":"Hi","reasoning":{"effort":"medium"},"max_output_tokens":256});
    let converted = convert::request(&source, false).unwrap();
    assert_eq!(converted.body["thinking"], json!({"type":"enabled"}));
    assert_eq!(converted.body["output_config"]["effort"], "high");
    let disabled =
        json!({"model":"deepseek-v4.1-flash","input":"Hi","reasoning":{"effort":"none"}});
    assert_eq!(
        convert::request(&disabled, false).unwrap().body["thinking"],
        json!({"type":"disabled"})
    );
    let source = json!({"model":"claude-haiku-4-5","input":"Hi","reasoning":{"effort":"high"},"max_output_tokens":8192});
    assert_eq!(
        convert::request(&source, false).unwrap().body["thinking"]["budget_tokens"],
        4096
    );
}

#[test]
fn unknown_models_require_thinking_evidence_and_manual_override_is_respected() {
    let source = json!({"model":"private-claude","input":"Hi","reasoning":{"effort":"high"}});
    assert!(convert::request(&source, false).is_err());
    let mut model = crate::types::ProviderModel {
        id: "private-claude".into(),
        ..Default::default()
    };
    model.profile_overrides.thinking_format = Some(crate::types::ThinkingFormat::Adaptive);
    assert_eq!(
        convert::request_with_model(&source, false, &model)
            .unwrap()
            .body["thinking"]["type"],
        "adaptive"
    );
    model.profile_overrides.thinking_format = Some(crate::types::ThinkingFormat::None);
    assert!(convert::request_with_model(&source, false, &model)
        .unwrap()
        .body
        .get("thinking")
        .is_none());
}

#[tokio::test]
async fn occupied_saved_port_recovers_without_changing_client_address_or_token() {
    let temp = tempfile::tempdir().unwrap();
    let (route, occupied) = Route::bind(temp.path()).unwrap();
    let persisted = std::fs::read(temp.path().join("protocol-bridge.json")).unwrap();
    let (same, listener) = Route::start(temp.path()).unwrap();
    assert!(listener.is_none());
    assert_eq!(same.port, route.port);
    assert_eq!(same.token, route.token);
    let store = Arc::new(Mutex::new(Store::open(temp.path().join("data")).unwrap()));
    drop(occupied);
    let task = tokio::spawn(maintain(listener, same, store));
    let deadline = std::time::Instant::now() + Duration::from_secs(5);
    while !healthy(&route).await && std::time::Instant::now() < deadline {
        tokio::time::sleep(Duration::from_millis(50)).await;
    }
    assert!(healthy(&route).await);
    let client = reqwest::Client::builder().no_proxy().build().unwrap();
    let url = format!("http://127.0.0.1:{}/health", route.port);
    assert_eq!(
        client.get(&url).send().await.unwrap().status(),
        StatusCode::UNAUTHORIZED
    );
    assert_eq!(
        client
            .get(&url)
            .bearer_auth(&route.token)
            .header("Origin", "http://other.test")
            .send()
            .await
            .unwrap()
            .status(),
        StatusCode::UNAUTHORIZED
    );
    assert_eq!(
        std::fs::read(temp.path().join("protocol-bridge.json")).unwrap(),
        persisted
    );
    task.abort();
}

#[test]
fn translates_history_tools_images_and_system() {
    let request = json!({"model":"claude-sonnet-4-6","instructions":"Be a coding agent.","tools":[{"type":"function","name":"shell_command","parameters":{"type":"object"}},{"type":"custom","name":"apply_patch","description":"Apply a patch."}],"input":[{"role":"developer","content":[{"type":"input_text","text":"Use UTF-8."}]},{"role":"user","content":[{"type":"input_text","text":"Fix this."},{"type":"input_image","image_url":"data:image/png;base64,AAAA"}]},{"type":"function_call","call_id":"tool_1","name":"shell_command","arguments":"{\"command\":\"pwd\"}"},{"type":"function_call_output","call_id":"tool_1","output":"D:/code"},{"type":"custom_tool_call","call_id":"tool_2","name":"apply_patch","input":"*** Begin Patch\n*** End Patch"},{"type":"custom_tool_call_output","call_id":"tool_2","output":"Success"}],"reasoning":{"effort":"high"},"parallel_tool_calls":false});
    let converted = convert::request(&request, true).unwrap();
    assert_eq!(converted.body["system"].as_array().unwrap().len(), 2);
    assert_eq!(
        converted.body["messages"][0]["content"][1]["source"]["type"],
        "base64"
    );
    assert_eq!(
        converted.body["messages"][1]["content"][0]["input"]["command"],
        "pwd"
    );
    assert_eq!(
        converted.body["messages"][2]["content"][0]["tool_use_id"],
        "tool_1"
    );
    assert_eq!(
        converted.body["messages"][3]["content"][0]["input"]["input"],
        "*** Begin Patch\n*** End Patch"
    );
    assert_eq!(converted.body["thinking"]["type"], "adaptive");
    assert!(converted.body.get("service_tier").is_none());
    assert_eq!(
        converted.body["tool_choice"]["disable_parallel_tool_use"],
        true
    );
}

#[test]
fn responses_continue_after_assistant_history_without_prefill() {
    let thinking =
        json!({"type":"thinking","thinking":"Preserve my plan","signature":"signed-history"});
    let redacted = json!({"type":"redacted_thinking","data":"opaque-history"});
    for model in [
        "claude-sonnet-4-6",
        "claude-haiku-5-5",
        "private-messages-model",
    ] {
        for stream in [false, true] {
            for phase in [Value::Null, json!("commentary"), json!("final_answer")] {
                let source = json!({"model":model,"input":[
                    {"role":"user","content":"Write the handbook."},
                    convert::item(&thinking, &[], "thinking_1").unwrap(),
                    convert::item(&redacted, &[], "thinking_2").unwrap(),
                    {"role":"assistant","phase":phase,"content":[{"type":"output_text","text":"Writing the optimization handbook to outputs."}]},
                    {"role":"assistant","content":"The existing progress must be preserved."}
                ]});
                let converted = convert::request(&source, stream).unwrap();
                let messages = converted.body["messages"].as_array().unwrap();
                assert_eq!(messages.last().unwrap()["role"], "user", "{model}: {phase}");
                assert_eq!(messages.len(), 3);
                assert_eq!(messages[0]["content"][0]["text"], "Write the handbook.");
                assert_eq!(messages[1]["role"], "assistant");
                assert_eq!(messages[1]["content"][0], thinking);
                assert_eq!(messages[1]["content"][1], redacted);
                assert_eq!(
                    messages[1]["content"][2]["text"],
                    "Writing the optimization handbook to outputs."
                );
                assert_eq!(
                    messages[1]["content"][3]["text"],
                    "The existing progress must be preserved."
                );
                assert!(!messages[2]["content"][0]["text"]
                    .as_str()
                    .unwrap()
                    .is_empty());
                assert_eq!(converted.body["stream"], stream);
            }
        }
    }
}

#[test]
fn responses_preserve_user_and_tool_result_endings() {
    let tools = json!([{"type":"function","name":"shell_command"}]);
    let history = json!([
        {"role":"user","content":"Read the file."},
        {"type":"function_call","name":"shell_command","call_id":"call_read","arguments":"{}"},
        {"type":"function_call_output","call_id":"call_read","output":"file contents"}
    ]);
    for input in [
        json!("Hello"),
        history.clone(),
        json!([
            {"role":"user","content":"Question"},
            {"role":"assistant","content":"Answer"},
            {"role":"user","content":"Next question"}
        ]),
    ] {
        let request = json!({"model":"claude-sonnet-4-6","input":input,"tools":tools});
        let converted = convert::request(&request, true).unwrap();
        let messages = converted.body["messages"].as_array().unwrap();
        assert_eq!(messages.len(), if input.is_string() { 1 } else { 3 });
        assert_eq!(messages.last().unwrap()["role"], "user");
        if input == history {
            assert_eq!(
                messages[2]["content"],
                json!([{"type":"tool_result","tool_use_id":"call_read","content":[{"type":"text","text":"file contents"}]}])
            );
        }
    }
    let thinking =
        json!({"type":"thinking","thinking":"Continue the plan","signature":"signed-history"});
    let converted = convert::request(
        &json!({"model":"claude-sonnet-4-6","input":[
            {"role":"user","content":"Continue"}, convert::item(&thinking, &[], "r1").unwrap()
        ]}),
        true,
    )
    .unwrap();
    assert_eq!(converted.body["messages"][1]["content"][0], thinking);
    assert_eq!(converted.body["messages"][2]["role"], "user");
}

#[test]
fn responses_reject_unanswered_tools_instead_of_inventing_results() {
    for kind in ["function_call", "custom_tool_call"] {
        let request = json!({"model":"claude-sonnet-4-6","input":[
            {"role":"user","content":"Read the file."},
            {"type":kind,"name":"read","call_id":"call_pending","arguments":"{}","input":"file.txt"},
            {"role":"assistant","phase":"commentary","content":"Waiting for the file."}
        ]});
        let error = convert::request(&request, true)
            .err()
            .expect("must require the pending tool result");
        assert!(error.contains("call_pending"), "{error}");
        assert!(error.contains("工具结果"), "{error}");
    }
}

#[test]
fn namespace_and_custom_tool_round_trip() {
    let source = json!({"model":"claude-opus-4-6","tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"apply_patch"}]}],"input":"hello"});
    let converted = convert::request(&source, true).unwrap();
    let block = json!({"type":"tool_use","id":"call_a","name":"functions__apply_patch","input":{"input":"patch\n汉字"}});
    let item = convert::item(&block, &converted.tools, "item_a").unwrap();
    assert_eq!(item["type"], "custom_tool_call");
    assert_eq!(item["namespace"], "functions");
    let mut round_trip = source;
    round_trip["input"] =
        json!([item,{"type":"custom_tool_call_output","call_id":"call_a","output":"done"}]);
    let converted = convert::request(&round_trip, false).unwrap();
    assert_eq!(converted.body["messages"][0]["content"][0], block);
}

#[test]
fn reasoning_and_compaction_replay_without_losing_signatures() {
    let thinking =
        json!({"type":"thinking","thinking":"Plan the change","signature":"opaque-signature"});
    let item = convert::item(&thinking, &[], "r1").unwrap();
    let converted = convert::request(&json!({"model":"claude-sonnet-4-6","input":[item,{"role":"assistant","content":"Answer"},{"role":"user","content":"Continue"}]}),true).unwrap();
    assert_eq!(converted.body["messages"][0]["content"][0], thinking);
    let compacted = convert::request(&json!({"model":"claude-sonnet-4-6","input":[{"type":"compaction","encrypted_content":convert::opaque_compaction("Keep the edited files")},{"role":"user","content":"continue"}]}),true).unwrap();
    assert!(compacted.body["messages"][0]["content"][0]["text"]
        .as_str()
        .unwrap()
        .contains("Keep the edited files"));
}

#[test]
fn stream_handles_byte_splits_thinking_tool_json_and_usage() {
    let converted = convert::request(&json!({"model":"claude-sonnet-4-6","input":"Hi","tools":[{"type":"function","name":"shell_command"}]}),true).unwrap();
    let mut translator = stream::Translator::new("claude-sonnet-4-6", converted.tools);
    let events = vec![
        json!({"type":"message_start","message":{"usage":{"input_tokens":12,"cache_read_input_tokens":10,"cache_creation_input_tokens":3}}}),
        json!({"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}),
        json!({"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"思考"}}),
        json!({"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"签名"}}),
        json!({"type":"content_block_stop","index":0}),
        json!({"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call1","name":"shell_command","input":{}}}),
        json!({"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"command\":"}}),
        json!({"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"pwd\"}"}}),
        json!({"type":"content_block_stop","index":1}),
        json!({"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":4}}),
        json!({"type":"message_stop"}),
    ];
    let wire = events
        .iter()
        .map(|e| format!("event: {}\r\ndata: {}\r\n\r\n", e["type"], e))
        .collect::<String>();
    let mut decoder = stream::SseDecoder::default();
    let mut output = translator.started();
    for byte in wire.as_bytes() {
        for event in decoder.feed(&[*byte]).unwrap() {
            output.extend(translator.accept(event).unwrap());
        }
    }
    let done = output.last().unwrap();
    assert_eq!(done["type"], "response.completed");
    assert_eq!(done["response"]["usage"]["input_tokens"], 25);
    assert_eq!(done["response"]["usage"]["total_tokens"], 29);
    assert_eq!(
        done["response"]["output"][1]["arguments"],
        "{\"command\":\"pwd\"}"
    );
    let replay = convert::request(&json!({"model":"claude-sonnet-4-6","tools":[{"type":"function","name":"shell_command"}],"input":[done["response"]["output"][0],done["response"]["output"][1],{"type":"function_call_output","call_id":"call1","output":"D:/code"}]}),true).unwrap();
    assert_eq!(
        replay.body["messages"][0]["content"][0]["signature"],
        "签名"
    );
    assert!(translator.finished);
}

#[test]
fn rejects_unsupported_inputs_and_invalid_tool_json() {
    for request in [
        json!({"model":"claude","input":"hello","previous_response_id":"resp_other"}),
        json!({"model":"claude","input":[{"role":"user","content":[{"type":"input_audio"}]}]}),
        json!({"model":"claude","input":"hello","tools":[{"type":"web_search"}]}),
        json!({"model":"claude","input":"hello","service_tier":"priority"}),
    ] {
        assert!(convert::request(&request, true).is_err());
    }
    let mut translator = stream::Translator::new("claude", vec![]);
    assert!(translator.accept(json!({"type":"message_stop"})).is_err());
    assert!(translator
        .accept(json!({"type":"error","error":{"message":"overloaded"}}))
        .is_err());
}

#[test]
fn old_records_default_to_direct_responses_and_urls_are_normalized() {
    let options: crate::types::CodexOptions = serde_json::from_value(json!({"models":[]})).unwrap();
    assert_eq!(options.protocol, crate::types::CodexProtocol::Openai);
    assert_eq!(
        endpoint("https://example.test/anthropic"),
        "https://example.test/anthropic/v1/messages"
    );
    assert_eq!(
        endpoint("https://example.test/v1/"),
        "https://example.test/v1/messages"
    );
    assert_eq!(
        endpoint("https://example.test/v1/messages"),
        "https://example.test/v1/messages"
    );
    assert_eq!(
        redact("rejected secret-key", "secret-key"),
        "rejected [已隐藏密钥]"
    );
}

#[tokio::test]
async fn responses_http_replays_assistant_history_without_prefill_for_json_and_sse() {
    use crate::types::{CodexOptions, CodexProtocol, Family, ProviderInput, ProviderModel, Target};
    let captured = Arc::new(Mutex::new(Vec::<Value>::new()));
    let upstream_requests = captured.clone();
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let base = format!("http://{}/v1", listener.local_addr().unwrap());
    let router = Router::new().route("/v1/messages", post(move |Json(body): Json<Value>| {
        let captured = upstream_requests.clone();
        async move {
            captured.lock().unwrap().push(body.clone());
            if body["messages"].as_array().unwrap().last().unwrap()["role"] == "assistant" {
                return (StatusCode::BAD_REQUEST, Json(json!({"error":{"message":"This model does not support assistant message prefill. The conversation must end with a user message."}}))).into_response();
            }
            let text = "QA_CONTINUATION_OK · 中文";
            if body["stream"] == true {
                let events = [
                    json!({"type":"message_start","message":{"usage":{"input_tokens":10,"output_tokens":0}}}),
                    json!({"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}),
                    json!({"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":text}}),
                    json!({"type":"content_block_stop","index":0}),
                    json!({"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}),
                    json!({"type":"message_stop"}),
                ];
                let wire = events.iter().map(|e| format!("event: {}\ndata: {e}\n\n", e["type"].as_str().unwrap())).collect::<String>();
                ([(header::CONTENT_TYPE, "text/event-stream")], wire).into_response()
            } else {
                Json(json!({"content":[{"type":"text","text":text}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5}})).into_response()
            }
        }
    }));
    let server = tokio::spawn(async move { axum::serve(listener, router).await.unwrap() });
    let temp = tempfile::tempdir().unwrap();
    let mut store = Store::open(temp.path().join("data")).unwrap();
    store
        .set_directory(
            Target::Codex,
            temp.path().join("isolated-codex").to_string_lossy().into(),
        )
        .unwrap();
    let token = "c".repeat(64);
    store.set_bridge_route(Route {
        port: 19874,
        token: token.clone(),
    });
    let provider = store
        .save(ProviderInput {
            id: None,
            family: Family::Codex,
            name: "Synthetic prefill QA".into(),
            base_url: base.clone(),
            api_key: Some("synthetic-prefill-key".into()),
            balance_access_token: None,
            model: "claude-sonnet-4-6".into(),
            auth_mode: "x-api-key".into(),
            reasoning_effort: None,
            codex_options: CodexOptions {
                protocol: CodexProtocol::Anthropic,
                models: vec![ProviderModel {
                    id: "claude-sonnet-4-6".into(),
                    enabled: true,
                    ..Default::default()
                }],
                ..Default::default()
            },
        })
        .unwrap();
    store.apply(Target::Codex, &provider.id).unwrap();
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
    let thinking = json!({"type":"thinking","thinking":"Keep the original plan","signature":"qa-signed-thinking"});
    let progress = json!({"role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"Writing the optimization handbook to outputs."}]});
    let source = json!({"model":provider.model,"input":[{"role":"user","content":"Write the handbook."},progress]});
    // A raw history conversion demonstrates the exact upstream error before
    // exercising the fixed HTTP handler, rather than accepting any 200 fixture.
    let old_wire = convert::compaction_request(&source).unwrap().body;
    let rejection = runtime
        .client
        .post(format!("{base}/messages"))
        .json(&old_wire)
        .send()
        .await
        .unwrap();
    assert_eq!(rejection.status(), StatusCode::BAD_REQUEST);
    assert!(rejection
        .text()
        .await
        .unwrap()
        .contains("assistant message prefill"));
    captured.lock().unwrap().clear();
    let tool_history = json!([
        {"role":"user","content":"Read the file."},
        {"type":"function_call","call_id":"call_done","name":"shell_command","arguments":"{}"},
        {"type":"function_call_output","call_id":"call_done","output":"File contents"}
    ]);
    for stream in [false, true] {
        for input in [
            json!("Hello"),
            source["input"].clone(),
            json!([{"role":"user","content":"Continue"},convert::item(&thinking, &[], "r1").unwrap()]),
            tool_history.clone(),
            json!([tool_history[0], tool_history[1], tool_history[2], progress]),
        ] {
            let count = captured.lock().unwrap().len();
            let response = responses(State(runtime.clone()), Path(provider.id.clone()), headers.clone(), Json(json!({"model":provider.model,"input":input,"stream":stream,"tools":[{"type":"function","name":"shell_command"}]}))).await.unwrap();
            assert_eq!(response.status(), StatusCode::OK);
            let bytes = axum::body::to_bytes(response.into_body(), 65536)
                .await
                .unwrap();
            let reply = std::str::from_utf8(&bytes).unwrap();
            assert!(reply.contains("QA_CONTINUATION_OK · 中文"), "{reply}");
            if stream {
                assert!(reply.contains("response.completed"));
                assert!(!reply.contains("event: error"));
            } else {
                let response: Value = serde_json::from_slice(&bytes).unwrap();
                assert_eq!(response["status"], "completed");
                assert_eq!(response["output"][0]["role"], "assistant");
            }
            let captured = captured.lock().unwrap();
            assert_eq!(captured.len(), count + 1, "one inference, no retries");
            let messages = captured.last().unwrap()["messages"].as_array().unwrap();
            assert_eq!(messages.last().unwrap()["role"], "user");
            if input == source["input"] {
                assert_eq!(
                    messages[1]["content"][0]["text"],
                    progress["content"][0]["text"]
                );
            }
            if input == tool_history {
                assert_eq!(
                    messages.last().unwrap()["content"]
                        .as_array()
                        .unwrap()
                        .len(),
                    1
                );
                assert_eq!(messages[2]["content"][0]["tool_use_id"], "call_done");
            }
        }
    }
    let count = captured.lock().unwrap().len();
    let pending = json!({"model":provider.model,"input":[{"role":"user","content":"Read"},{"type":"function_call","name":"shell_command","call_id":"call_pending","arguments":"{}"}],"tools":[{"type":"function","name":"shell_command"}]});
    let error = responses(
        State(runtime.clone()),
        Path(provider.id.clone()),
        headers.clone(),
        Json(pending.clone()),
    )
    .await
    .err()
    .unwrap();
    assert_eq!(error.0, StatusCode::BAD_REQUEST);
    assert!(error.1 .0["error"]["message"]
        .as_str()
        .unwrap()
        .contains("call_pending"));
    assert_eq!(
        captured.lock().unwrap().len(),
        count,
        "pending tools must not reach upstream"
    );
    // Compaction may summarize an unfinished tool as data; generation's pending
    // tool guard and continuation instruction must not change that behavior.
    let summary = compact(State(runtime), Path(provider.id), headers, Json(pending))
        .await
        .unwrap();
    assert_eq!(summary.0["object"], "response.compaction");
    let captured = captured.lock().unwrap();
    let body = captured.last().unwrap();
    assert!(body.get("tools").is_none());
    let messages = body["messages"].as_array().unwrap();
    assert_eq!(messages.len(), 3);
    assert!(messages[1]["content"][0]["text"]
        .as_str()
        .unwrap()
        .starts_with("Tool history (data):"));
    assert!(messages[2]["content"][0]["text"]
        .as_str()
        .unwrap()
        .starts_with("Summarize the conversation"));
    assert_eq!(messages[2]["content"].as_array().unwrap().len(), 1);
    server.abort();
}
