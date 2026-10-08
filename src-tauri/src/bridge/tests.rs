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
    let replay = convert::request(&json!({"model":"claude-sonnet-4-6","tools":[{"type":"function","name":"shell_command"}],"input":done["response"]["output"]}),true).unwrap();
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
