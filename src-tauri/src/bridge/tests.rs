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
                assert_eq!(
                    response["output"].as_array().unwrap().last().unwrap()["role"],
                    "assistant"
                );
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

#[test]
fn usage_audit_sse_accepts_cr_line_endings_and_split_crlf() {
    for wire in [
        "data: {\"type\":\"ping\"}\r\r",
        "data: {\"type\":\"ping\"}\r\n\r\n",
        "data: {\"type\":\"ping\"}\r\n\n",
        "data: {\"type\":\"ping\"}\n\r\n",
        "data: {\rdata: \"type\":\"ping\"\rdata: }\r\r",
    ] {
        let mut decoder = stream::SseDecoder::default();
        let mut events = Vec::new();
        for byte in wire.as_bytes() {
            events.extend(decoder.feed(&[*byte]).unwrap());
        }
        assert_eq!(events, vec![json!({"type":"ping"})]);
        decoder.finish().unwrap();
    }
}

#[test]
fn usage_audit_sse_limits_whole_events_across_many_data_lines() {
    let mut decoder = stream::SseDecoder::default();
    let line = format!("data: \"{}\",\n", "a".repeat(512 * 1024));
    assert!(decoder.feed(b"data: [\n").unwrap().is_empty());
    let rejected = (0..9).any(|_| decoder.feed(line.as_bytes()).is_err());
    assert!(
        rejected,
        "an unfinished multi-line event must not grow without a limit"
    );
}

#[test]
fn usage_audit_sse_handles_bom_batched_events_and_rejects_truncated_tail() {
    let mut decoder = stream::SseDecoder::default();
    let wire = "\u{feff}data: {\"type\":\"ping\"}\n\n";
    let mut events = Vec::new();
    for byte in wire.as_bytes() {
        events.extend(decoder.feed(&[*byte]).unwrap());
    }
    assert_eq!(events.len(), 1);
    decoder.finish().unwrap();
    let event = "data: {\"type\":\"ping\"}\n\n";
    let count = 4 * 1024 * 1024 / event.len() + 1;
    let batched = event.repeat(count);
    assert!(batched.len() > 4 * 1024 * 1024);
    assert_eq!(decoder.feed(batched.as_bytes()).unwrap().len(), count);
    decoder.feed(b"data: {\"choices\":").unwrap();
    assert!(decoder.finish().is_err());
}

#[test]
fn claude_55_adaptive_efforts_defaults_and_forced_tools() {
    for model in ["claude-opus-5-5", "claude-sonnet-5-5", "claude-fable-5-1"] {
        for effort in [
            None,
            Some("minimal"),
            Some("low"),
            Some("medium"),
            Some("high"),
            Some("xhigh"),
            Some("max"),
            Some("ultra"),
        ] {
            let mut source = json!({"model":model,"input":"Hi","max_output_tokens":256,"temperature":0.2,"top_p":0.8});
            if let Some(effort) = effort {
                source["reasoning"] = json!({"effort":effort});
            }
            let body = convert::request(&source, false).unwrap().body;
            assert_eq!(body["model"], model);
            assert_eq!(
                body["thinking"],
                json!({"type":"adaptive","display":"summarized"})
            );
            assert!(body.get("temperature").is_none());
            assert!(body.get("top_p").is_none());
            assert!(body["thinking"].get("budget_tokens").is_none());
            if let Some(effort) = effort {
                assert_eq!(
                    body["output_config"]["effort"],
                    match effort {
                        "minimal" => "low",
                        "ultra" => "max",
                        e => e,
                    }
                );
            }
        }
        for choice in [
            json!("required"),
            json!({"type":"function","name":"qa_echo"}),
        ] {
            let result = convert::request(
                &json!({"model":model,"input":"Hi","tools":[{"type":"function","name":"qa_echo"}],"tool_choice":choice}),
                false,
            );
            assert!(result.err().unwrap().contains("强制工具"));
        }
        let body = convert::request(&json!({"model":model,"input":"Hi","tools":[{"type":"function","name":"qa_echo"}],"tool_choice":"auto","parallel_tool_calls":false}),false).unwrap().body;
        assert_eq!(body["tool_choice"]["disable_parallel_tool_use"], true);
    }
    let body = convert::request(
        &json!({"model":"claude-sonnet-5-5","input":"Hi","reasoning":{"effort":"none"}}),
        false,
    )
    .unwrap()
    .body;
    assert_eq!(body["thinking"], json!({"type":"between_tools"}));
    assert_eq!(body["output_config"]["effort"], "low");
    assert!(convert::request(
        &json!({"model":"claude-opus-5-5","input":"Hi","reasoning":{"effort":"none"}}),
        false
    )
    .err()
    .unwrap()
    .contains("始终"));
}

#[test]
fn signature_only_and_initial_thinking_streams_replay_losslessly() {
    for initial in [
        json!({"type":"thinking","thinking":"","signature":""}),
        json!({"type":"thinking","signature":""}),
        json!({"type":"thinking","thinking":"Initial plan","signature":""}),
    ] {
        let mut translator = stream::Translator::new("claude-sonnet-5-5", vec![]);
        let mut output = translator.started();
        for event in [
            json!({"type":"message_start","message":{"usage":{"input_tokens":1}}}),
            json!({"type":"content_block_start","index":0,"content_block":initial}),
            json!({"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":""}}),
            json!({"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"signed-"}}),
            json!({"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"history"}}),
            json!({"type":"content_block_stop","index":0}),
            json!({"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}),
            json!({"type":"message_stop"}),
        ] {
            output.extend(translator.accept(event).unwrap());
        }
        let item = &output.last().unwrap()["response"]["output"][0];
        let summary_count = output
            .iter()
            .filter(|v| v["type"] == "response.reasoning_summary_part.added")
            .count();
        assert_eq!(
            summary_count,
            usize::from(initial["thinking"].as_str().is_some_and(|s| !s.is_empty()))
        );
        assert_eq!(item["summary"].as_array().unwrap().len(), summary_count);
        let replay = convert::request(&json!({"model":"claude-sonnet-5-5","input":[item,{"role":"user","content":"Continue"}]}),false).unwrap();
        let mut expected = initial.clone();
        expected["signature"] = json!("signed-history");
        assert_eq!(replay.body["messages"][0]["content"][0], expected);
    }
}

#[test]
fn open_capabilities_preserve_explicit_serial_tool_requests() {
    let mut model = crate::types::ProviderModel {
        id: "custom-model".into(),
        ..Default::default()
    };
    model.profile.tool_calls = Some(false);
    model.profile.structured_output = Some(false);
    for choice in [
        None,
        Some(json!({"type":"auto","disable_parallel_tool_use":false})),
        Some(json!({"type":"auto","disable_parallel_tool_use":true})),
    ] {
        let mut body = json!({"model":"custom-model","messages":[{"role":"user","content":"Hi"}],"tools":[{"name":"qa_echo","input_schema":{"type":"object"}}],"output_config":{"format":{"type":"json_schema","schema":{"type":"object"}}}});
        if let Some(value) = choice.clone() {
            body["tool_choice"] = value;
        }
        let converted = reverse::request_with_model(&body, "custom-model", false, &model).unwrap();
        assert_eq!(
            converted["parallel_tool_calls"],
            choice
                .as_ref()
                .is_none_or(|v| v["disable_parallel_tool_use"] != true)
        );
        assert!(converted.get("tools").is_some());
        assert!(converted.get("text").is_some());
    }
}

#[test]
fn inline_system_updates_preserve_signed_prefix_and_legacy_behavior() {
    let thinking = json!({"type":"thinking","signature":"qa-bound-prefix"});
    let initial = json!({"model":"claude-sonnet-5-5","instructions":"Initial","input":[
        {"role":"developer","content":"Initial developer"},
        {"role":"user","content":"First request"},
        convert::item(&thinking, &[], "r1").unwrap(),
        {"role":"assistant","content":"First reply"},
        {"role":"user","content":"Second request"}
    ]});
    let before = convert::request(&initial, false).unwrap().body;
    let mut next = initial.clone();
    next["input"].as_array_mut().unwrap().extend([
        json!({"role":"developer","content":"New instructions"}),
        json!({"role":"system","content":"Keep Chinese: 中文"}),
    ]);
    let result = convert::request(&next, false).unwrap().body;
    assert_eq!(result["system"], before["system"]);
    let messages = result["messages"].as_array().unwrap();
    assert_eq!(&messages[..3], before["messages"].as_array().unwrap());
    assert_eq!(messages[3]["role"], "system");
    assert_eq!(messages[3]["content"].as_array().unwrap().len(), 2);
    assert_eq!(messages[1]["content"][0], thinking);
    // Older gateways retain the historical top-level system format.
    next["model"] = json!("claude-sonnet-4-5");
    let old = convert::request(&next, false).unwrap().body;
    assert_eq!(old["system"].as_array().unwrap().len(), 4);
    assert_eq!(old["messages"].as_array().unwrap().len(), 3);
    // An inline system group cannot split assistant blocks or precede a user.
    next["model"] = json!("claude-sonnet-5-5");
    next["input"]
        .as_array_mut()
        .unwrap()
        .push(json!({"role":"user","content":"Invalid placement"}));
    assert!(convert::request(&next, false).is_err());
    next["input"] = json!([{"role":"user","content":"QA"},{"role":"assistant","content":"Reply"},{"role":"system","content":"Invalid placement"}]);
    assert!(convert::request(&next, false).is_err());
    next["input"] = json!([
        {"role":"user","content":"QA"},
        {"type":"function_call","call_id":"c1","name":"Read","arguments":"{}"},
        {"type":"function_call_output","call_id":"c1","output":"Done"},
        {"role":"system","content":"New instructions"}
    ]);
    let result = convert::request(&next, false).unwrap().body;
    assert_eq!(result["messages"][2]["content"][0]["type"], "tool_result");
    assert_eq!(result["messages"][3]["role"], "system");
}

#[tokio::test]
async fn continuation_http_keeps_bound_history_across_json_sse_and_tool_rounds() {
    use crate::types::{CodexOptions, CodexProtocol, Family, ProviderInput, ProviderModel, Target};
    use sha2::{Digest, Sha256};
    fn bound_signature(body: &Value, length: usize) -> String {
        let prefix = json!({"model":body["model"],"system":body["system"],"tools":body["tools"],"messages":&body["messages"].as_array().unwrap()[..length]});
        format!(
            "qa-bound-{:x}",
            Sha256::digest(prefix.to_string().as_bytes())
        )
    }
    let captured = Arc::new(Mutex::new(Vec::<Value>::new()));
    let upstream_requests = captured.clone();
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let base = format!("http://{}/v1", listener.local_addr().unwrap());
    let router = Router::new().route("/v1/messages", post(move |Json(body): Json<Value>| {
        let captured = upstream_requests.clone();
        async move {
            captured.lock().unwrap().push(body.clone());
            let messages = body["messages"].as_array().unwrap();
            for (index, message) in messages.iter().enumerate() {
                if message["role"] == "system" && (index == 0 || messages[index-1]["role"] != "user"
                    || messages.get(index+1).is_some_and(|next| next["role"] != "assistant"))
                {
                    return (StatusCode::BAD_REQUEST,Json(json!({"error":{"message":"QA invalid inline system placement"}}))).into_response();
                }
            }
            for (index, message) in messages.iter().enumerate() {
                for block in message["content"].as_array().unwrap() {
                    if block["signature"].as_str().is_some_and(|s| s.starts_with("qa-bound-"))
                        && block["signature"] != bound_signature(&body, index)
                    {
                        return (StatusCode::BAD_REQUEST, Json(json!({"error":{"message":"QA thinking prefix changed"}}))).into_response();
                    }
                }
            }
            let thinking = json!({"type":"thinking","signature":bound_signature(&body, messages.len())});
            let run_tool = messages.iter().rev().find(|m| m["role"] == "user")
                .is_some_and(|m| m["content"][0]["text"] == "RUN_TOOL");
            let answer = if run_tool {
                json!({"type":"tool_use","id":"qa_call","name":"Read","input":{"path":"qa.txt"}})
            } else {
                json!({"type":"text","text":"QA_ROUND_OK · 中文"})
            };
            let stop = if run_tool {"tool_use"} else {"end_turn"};
            if body["stream"] == true {
                let mut initial = answer.clone();
                if run_tool { initial["input"] = json!({}); } else { initial["text"] = json!(""); }
                let delta = if run_tool {
                    json!({"type":"input_json_delta","partial_json":answer["input"].to_string()})
                } else {
                    json!({"type":"text_delta","text":answer["text"]})
                };
                let events = [
                    json!({"type":"message_start","message":{"usage":{"input_tokens":10,"output_tokens":0}}}),
                    json!({"type":"content_block_start","index":0,"content_block":{"type":"thinking","signature":""}}),
                    json!({"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":thinking["signature"]}}),
                    json!({"type":"content_block_stop","index":0}),
                    json!({"type":"content_block_start","index":1,"content_block":initial}),
                    json!({"type":"content_block_delta","index":1,"delta":delta}),
                    json!({"type":"content_block_stop","index":1}),
                    json!({"type":"message_delta","delta":{"stop_reason":stop},"usage":{"output_tokens":5}}),
                    json!({"type":"message_stop"}),
                ];
                let wire = events.iter().map(|e| format!("event: {}\ndata: {e}\n\n", e["type"].as_str().unwrap())).collect::<String>();
                ([(header::CONTENT_TYPE, "text/event-stream")], wire).into_response()
            } else {
                Json(json!({"content":[thinking,answer],"stop_reason":stop,"usage":{"input_tokens":10,"output_tokens":5}})).into_response()
            }
        }
    }));
    let server = tokio::spawn(async move { axum::serve(listener, router).await.unwrap() });
    let temp = tempfile::tempdir().unwrap();
    let mut store = Store::open(temp.path().join("data")).unwrap();
    store
        .set_directory(
            Target::Codex,
            temp.path().join("codex").to_string_lossy().into(),
        )
        .unwrap();
    let token = "d".repeat(64);
    store.set_bridge_route(Route {
        port: 19875,
        token: token.clone(),
    });
    let provider = store
        .save(ProviderInput {
            id: None,
            family: Family::Codex,
            name: "Synthetic signed history QA".into(),
            base_url: base,
            api_key: Some("synthetic-bound-key".into()),
            balance_access_token: None,
            model: "claude-sonnet-5-5".into(),
            auth_mode: "x-api-key".into(),
            reasoning_effort: None,
            codex_options: CodexOptions {
                protocol: CodexProtocol::Anthropic,
                models: vec![ProviderModel {
                    id: "claude-sonnet-5-5".into(),
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
    // Mix transports between rounds: either response must replay identically.
    for start_streaming in [false, true] {
        let mut input = vec![
            json!({"role":"user","content":"Start"}),
            json!({"role":"assistant","content":"Working"}),
        ];
        for round in 0..5 {
            if round == 2 {
                input.extend([
                    json!({"role":"user","content":"RUN_TOOL"}),
                    json!({"role":"developer","content":"New tool instructions"}),
                ]);
            } else if round == 3 {
                input.push(json!({"type":"function_call_output","call_id":"qa_call","output":"Synthetic contents"}));
            } else if round == 4 {
                input.extend([
                    json!({"role":"user","content":"Follow up"}),
                    json!({"role":"system","content":"New final instructions"}),
                ]);
            }
            let streaming = start_streaming != (round % 2 == 1);
            let body = json!({"model":provider.model,"instructions":"Stable initial instructions","input":input,"stream":streaming,"tools":[{"type":"function","name":"Read","parameters":{"type":"object"}}]});
            let count = captured.lock().unwrap().len();
            let reply = responses(
                State(runtime.clone()),
                Path(provider.id.clone()),
                headers.clone(),
                Json(body.clone()),
            )
            .await
            .unwrap();
            let bytes = axum::body::to_bytes(reply.into_body(), 65536)
                .await
                .unwrap();
            let result = if streaming {
                let mut decoder = stream::SseDecoder::default();
                let events = decoder.feed(&bytes).unwrap();
                decoder.finish().unwrap();
                assert!(!events.iter().any(|e| e["type"] == "error"), "{events:?}");
                let completed = events
                    .iter()
                    .find(|e| e["type"] == "response.completed")
                    .unwrap()["response"]
                    .clone();
                for event in events
                    .iter()
                    .filter(|e| e["type"] == "response.output_item.done")
                {
                    let index = event["output_index"].as_u64().unwrap() as usize;
                    assert_eq!(event["item"], completed["output"][index]);
                }
                assert!(!events.iter().any(|e| e["type"]
                    .as_str()
                    .unwrap()
                    .starts_with("response.reasoning_summary")));
                completed
            } else {
                serde_json::from_slice::<Value>(&bytes).unwrap()
            };
            assert_eq!(result["status"], "completed");
            assert_eq!(
                captured.lock().unwrap().len(),
                count + 1,
                "no inference retries"
            );
            let output = result["output"].as_array().unwrap();
            assert_eq!(output.len(), if round < 2 { 3 } else { 2 });
            input.extend(output.iter().cloned());
            if round == 1 {
                // Demonstrate the old bug: dropping the bridge-generated turn
                // invalidates signatures on the next upstream inference.
                let mut broken = body;
                broken["stream"] = json!(false);
                let mut missing_marker = input.clone();
                missing_marker
                    .retain(|item| item["encrypted_content"] != "uni-anthropic-continuation:1");
                broken["input"] = json!(missing_marker);
                let error = responses(
                    State(runtime.clone()),
                    Path(provider.id.clone()),
                    headers.clone(),
                    Json(broken),
                )
                .await
                .err()
                .unwrap();
                assert_eq!(error.0, StatusCode::BAD_REQUEST);
                assert!(error.1 .0["error"]["message"]
                    .as_str()
                    .unwrap()
                    .contains("prefix changed"));
            }
        }
        // Compaction replays recorded turns, but adds no generation marker.
        let converted=convert::compaction_request(&json!({"model":provider.model,"instructions":"Stable initial instructions","input":input})).unwrap();
        assert!(!converted.continuation);
        assert_eq!(
            converted.body["messages"]
                .as_array()
                .unwrap()
                .last()
                .unwrap()["role"],
            "assistant"
        );
        let mut trailing = input.clone();
        trailing.extend([
            json!({"role":"user","content":"Prepare summary"}),
            json!({"role":"developer","content":"Keep current constraints"}),
        ]);
        let thinking_only = json!([
            {"role":"user","content":"Start"},
            {"role":"system","content":"Historical constraints"},
            convert::item(&json!({"type":"thinking","signature":"qa-compaction-only"}),&[],"qa_thinking").unwrap(),
            {"role":"user","content":"Prepare summary"},
            {"role":"system","content":"Keep current constraints"}
        ]);
        for history in [json!(input), json!(trailing), thinking_only] {
            let summary=compact(State(runtime.clone()),Path(provider.id.clone()),headers.clone(),Json(json!({"model":provider.model,"instructions":"Stable initial instructions","input":history}))).await.unwrap();
            assert_eq!(summary.0["object"], "response.compaction");
            let captured = captured.lock().unwrap();
            let body = captured.last().unwrap();
            assert!(body.get("tools").is_none());
            assert!(!body["messages"].to_string().contains("qa-bound-"));
            let messages = body["messages"].as_array().unwrap();
            let prompt = messages
                .iter()
                .rev()
                .find(|m| m["role"] != "system")
                .unwrap();
            assert_eq!(prompt["role"], "user");
            assert!(
                prompt["content"].as_array().unwrap().last().unwrap()["text"]
                    .as_str()
                    .unwrap()
                    .starts_with("Summarize the conversation")
            );
            if history.as_array().unwrap().last().unwrap()["role"] != "assistant" {
                assert_eq!(messages.last().unwrap()["role"], "system");
                assert_eq!(
                    messages.last().unwrap()["content"][0]["text"],
                    "Keep current constraints"
                );
            }
        }
    }
    server.abort();
}

#[test]
fn continuation_markers_reject_invalid_placement_versions_and_pending_tools() {
    for input in [
        json!([convert::continuation_item()]),
        json!([{"role":"user","content":"QA"},convert::continuation_item()]),
        json!([{"role":"user","content":"QA"},{"role":"assistant","content":"Reply"},convert::continuation_item(),convert::continuation_item()]),
        json!([{"type":"function_call","call_id":"pending","name":"Read","arguments":"{}"},convert::continuation_item()]),
        json!([{"role":"assistant","content":"Reply"},{"type":"reasoning","encrypted_content":"uni-anthropic-continuation:2","summary":[]}]),
    ] {
        assert!(
            convert::request(&json!({"model":"claude-sonnet-5-5","input":input}), false).is_err()
        );
    }
}
