use super::*;

#[test]
fn mid_conversation_system_messages_keep_roles_order_and_tool_history() {
    let source = json!({"system":"Initial instructions","max_tokens":256,"messages":[
        {"role":"user","content":"Read the file"},
        {"role":"assistant","content":[{"type":"tool_use","id":"call_system","name":"Read","input":{"path":"qa.txt"}}]},
        {"role":"user","content":[{"type":"tool_result","tool_use_id":"call_system","content":"Synthetic contents"}]},
        {"role":"system","content":[{"type":"text","text":"New instructions","cache_control":{"type":"ephemeral"}},{"type":"text","text":"Keep Chinese: 中文"}]},
        {"role":"assistant","content":"Acknowledged"},
        {"role":"system","content":"Latest instructions"},
        {"role":"user","content":"Continue"}
    ]});
    for streaming in [false, true] {
        let converted = request(&source, "gpt-4.1", streaming).unwrap();
        assert_eq!(converted["instructions"], "Initial instructions");
        assert_eq!(converted["input"].as_array().unwrap().len(), 7);
        assert_eq!(converted["input"][1]["type"], "function_call");
        assert_eq!(converted["input"][2]["type"], "function_call_output");
        assert_eq!(
            converted["input"][3],
            json!({"type":"message","role":"system","content":[{"type":"input_text","text":"New instructions"},{"type":"input_text","text":"Keep Chinese: 中文"}]})
        );
        assert_eq!(converted["input"][5]["role"], "system");
        assert_eq!(converted["input"][6]["content"][0]["text"], "Continue");
        let chat = chat_request(&converted).unwrap();
        let roles: Vec<_> = chat["messages"]
            .as_array()
            .unwrap()
            .iter()
            .map(|m| m["role"].as_str().unwrap())
            .collect();
        assert_eq!(
            roles,
            [
                "system",
                "user",
                "assistant",
                "tool",
                "system",
                "assistant",
                "system",
                "user"
            ]
        );
        assert_eq!(chat["messages"][2]["tool_calls"][0]["id"], "call_system");
        assert_eq!(chat["messages"][3]["tool_call_id"], "call_system");
        assert_eq!(
            chat["messages"][4]["content"][1]["text"],
            "Keep Chinese: 中文"
        );
        assert_eq!(
            chat["messages"][6]["content"][0]["text"],
            "Latest instructions"
        );
        assert_eq!(chat["stream"], streaming);
    }
}

#[test]
fn temporary_system_text_expires_without_losing_per_turn_effort() {
    let mut model = crate::types::ProviderModel {
        id: "gpt-5.4".into(),
        ..Default::default()
    };
    model.profile_overrides.reasoning_efforts =
        Some(vec!["low".into(), "medium".into(), "max".into()]);
    let mut source = json!({"max_tokens":256,"thinking":{"type":"adaptive"},"output_config":{"effort":"medium"},"messages":[
        {"role":"user","content":"First turn"},
        {"role":"system","content":"Expired instruction","clear_at":"next_user_message","output_config":{"effort":"low"}},
        {"role":"assistant","content":"First reply"},
        {"role":"user","content":"Second turn"},
        {"role":"system","content":[{"type":"text","text":"Current instruction"}],"clear_at":"next_user_message","output_config":{}},
        {"role":"system","content":[],"clear_at":null,"output_config":{"effort":"max"}},
        {"role":"system","content":"Persistent instruction","clear_at":"never","output_config":{"effort":null}}
    ]});
    let converted = request_with_model(&source, &model.id, false, &model).unwrap();
    assert_eq!(converted["input"].as_array().unwrap().len(), 5);
    assert_eq!(
        converted["input"][3]["content"][0]["text"],
        "Current instruction"
    );
    assert_eq!(
        converted["input"][4]["content"][0]["text"],
        "Persistent instruction"
    );
    assert!(!converted.to_string().contains("Expired instruction"));
    assert_eq!(converted["reasoning"]["effort"], "max");
    assert_eq!(
        chat_request_with_model(&converted, &model).unwrap()["reasoning_effort"],
        "max"
    );
    source["messages"][5]["output_config"] = json!({});
    let converted = request_with_model(&source, &model.id, false, &model).unwrap();
    assert_eq!(converted["reasoning"]["effort"], "low");
}

#[test]
fn rejects_unknown_roles_and_non_text_system_content_without_echoing_input() {
    for role in [
        json!("developer"),
        json!("tool"),
        json!("synthetic-secret-role"),
        json!(42),
        Value::Null,
    ] {
        let invalid = json!({"messages":[{"role":role,"content":"synthetic-secret-content"}]});
        let error = request(&invalid, "gpt-4.1", false).unwrap_err();
        assert!(!error.contains("synthetic-secret"));
    }
    for content in [
        json!([{"type":"image","source":{"type":"url","url":"https://example.test/synthetic-secret"}}]),
        json!([{"type":"tool_use","id":"call1","name":"Read","input":{}}]),
        json!([{"type":"tool_result","tool_use_id":"call1","content":"Done"}]),
        json!([{"type":"thinking","thinking":"synthetic-secret"}]),
        json!([{"type":"tool_reference","tool_name":"Read"}]),
        json!([{"type":"text","text":42}]),
        json!({"text":"synthetic-secret"}),
        Value::Null,
    ] {
        let invalid = json!({"messages":[{"role":"system","content":content},{"role":"user","content":"QA"}]});
        let error = request(&invalid, "gpt-4.1", false).unwrap_err();
        assert!(error.contains("system"), "{error}");
        assert!(!error.contains("synthetic-secret"));
        let invalid_top = json!({"system":content,"messages":[{"role":"user","content":"QA"}]});
        assert!(request(&invalid_top, "gpt-4.1", false).is_err());
    }
    for fields in [
        json!({"clear_at":"unknown"}),
        json!({"clear_at":42}),
        json!({"output_config":"unknown"}),
        json!({"output_config":{"format":{"type":"json_schema"}}}),
        json!({"output_config":{"effort":"unknown"}}),
        json!({"output_config":{"effort":42}}),
    ] {
        let mut message = json!({"role":"system","content":"QA"});
        message
            .as_object_mut()
            .unwrap()
            .extend(fields.as_object().unwrap().clone());
        let invalid = json!({"messages":[message,{"role":"user","content":"QA"}]});
        assert!(request(&invalid, "gpt-4.1", false).is_err());
    }
}

#[test]
fn output_config_only_system_message_changes_effort_without_creating_history() {
    let source = json!({"messages":[
        {"role":"user","content":"QA"},
        {"role":"system","output_config":{"effort":"low"}}
    ],"thinking":{"type":"adaptive"},"output_config":{"effort":"high"},"max_tokens":256});
    let converted = request(&source, "gpt-5.4", false).unwrap();
    assert_eq!(converted["input"].as_array().unwrap().len(), 1);
    assert_eq!(converted["reasoning"]["effort"], "low");
    for config in [json!({}), json!({"effort":null})] {
        let mut invalid = source.clone();
        invalid["messages"][1]["output_config"] = config;
        assert!(request(&invalid, "gpt-5.4", false).is_err());
    }
}

#[test]
fn deepseek_chat_maps_effort_and_replays_reasoning_on_tool_turns() {
    let model = crate::types::ProviderModel {
        id: "deepseek-flash".into(),
        ..Default::default()
    };
    let reply = response(&json!({"choices":[{"message":{"reasoning_content":"Plan","tool_calls":[{"id":"call1","function":{"name":"Read","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}), "deepseek-flash", true).unwrap();
    let source = json!({"messages":[{"role":"assistant","content":reply["content"]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call1","content":"Done"}]}],"thinking":{"type":"adaptive"},"output_config":{"effort":"medium"},"max_tokens":256,"temperature":0.8});
    let converted = request_with_model(&source, "deepseek-flash", false, &model).unwrap();
    assert_eq!(converted["reasoning"], json!({"effort":"high"}));
    assert!(converted.get("include").is_none());
    assert!(converted.get("temperature").is_none());
    let chat = chat_request_with_model(&converted, &model).unwrap();
    assert_eq!(chat["thinking"]["type"], "enabled");
    assert_eq!(chat["reasoning_effort"], "high");
    assert_eq!(chat["messages"][0]["reasoning_content"], "Plan");
    assert_eq!(chat["messages"][0]["tool_calls"][0]["id"], "call1");
    let disabled = request_with_model(
        &json!({"messages":[{"role":"user","content":"Hi"}],"max_tokens":256}),
        "deepseek-flash",
        false,
        &model,
    )
    .unwrap();
    let disabled = chat_request_with_model(&disabled, &model).unwrap();
    assert_eq!(disabled["thinking"]["type"], "disabled");
    assert!(disabled.get("reasoning_effort").is_none());
    let mut explicit = model;
    explicit.profile_overrides.reasoning_efforts = Some(vec!["max".into()]);
    let request = request_with_model(&source, "deepseek-flash", false, &explicit).unwrap();
    assert_eq!(request["reasoning"]["effort"], "max");
}

#[test]
fn chat_thinking_stream_emits_replayable_signature() {
    let mut translator = Translator::new("deepseek-flash");
    translator
        .accept_chat(
            json!({"choices":[{"delta":{"reasoning_content":"Plan"},"finish_reason":"stop"}]}),
        )
        .unwrap();
    let events = translator.finish_chat().unwrap();
    let signed = events
        .iter()
        .find(|event| event["delta"]["type"] == "signature_delta")
        .unwrap();
    assert!(signed["delta"]["signature"]
        .as_str()
        .unwrap()
        .starts_with(REASONING_PREFIX));
    assert_eq!(
        translator.content[0]["signature"],
        signed["delta"]["signature"]
    );
}

#[test]
fn converts_system_images_tools_errors_and_signed_reasoning() {
    let reasoning = json!({"type":"reasoning","id":"rs_qa","summary":[{"type":"summary_text","text":"Plan"}],"encrypted_content":"encrypted_qa"});
    let source = json!({"model":"gpt-5.4","system":[{"type":"text","text":"Code carefully"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"max"},"max_tokens":32000,
        "tools":[{"name":"Read","input_schema":{"type":"object","properties":{"file_path":{"type":"string"}}},"defer_loading":true},{"type":"tool_search_tool_regex_20251119","name":"ToolSearch"}],
        "tool_choice":{"type":"tool","name":"Read","disable_parallel_tool_use":true},
        "messages":[{"role":"user","content":[{"type":"text","text":"Read"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]},
            {"role":"assistant","content":[{"type":"thinking","thinking":"Plan","signature":signature(&reasoning)},{"type":"tool_use","id":"call1","name":"Read","input":{"file_path":"qa.txt"}}]},
            {"role":"user","content":[{"type":"tool_result","tool_use_id":"call1","is_error":true,"content":"File missing"}]}]});
    let request = request(&source, "gpt-5.4", true).unwrap();
    assert_eq!(request["instructions"], "Code carefully");
    assert_eq!(
        request["input"][0]["content"][1]["image_url"],
        "data:image/png;base64,AAAA"
    );
    assert_eq!(request["input"][1], reasoning);
    assert_eq!(request["input"][2]["type"], "function_call");
    assert_eq!(
        request["input"][3]["output"][0]["text"],
        "Tool execution failed:"
    );
    assert_eq!(request["tools"].as_array().unwrap().len(), 1);
    assert_eq!(request["reasoning"]["effort"], "high");
    assert_eq!(request["parallel_tool_calls"], false);
    assert_eq!(request["store"], false);
    let chat = chat_request(&request).unwrap();
    assert_eq!(chat["messages"][2]["tool_calls"][0]["id"], "call1");
    assert_eq!(chat["messages"][3]["role"], "tool");
    assert_eq!(chat["max_completion_tokens"], 32000);
    assert_eq!(chat["stream_options"]["include_usage"], true);
}

#[test]
fn responses_sse_tools_preserve_json_whitespace_and_reasoning_signature() {
    let mut translator = Translator::new("gpt-5.4");
    let reason = json!({"id":"rs_qa","type":"reasoning","summary":[{"type":"summary_text","text":"思考"}],"encrypted_content":"encrypted_qa"});
    let tool = json!({"id":"fc_qa","type":"function_call","call_id":"call1","name":"Read","arguments":"{ \"file_path\" : \"qa.txt\" }"});
    let events = [
        json!({"type":"response.reasoning_summary_text.delta","output_index":0,"delta":"思考"}),
        json!({"type":"response.output_item.done","output_index":0,"item":reason}),
        json!({"type":"response.output_item.added","output_index":1,"item":tool}),
        json!({"type":"response.function_call_arguments.delta","output_index":1,"delta":"{ \"file_path\" : "}),
        json!({"type":"response.function_call_arguments.delta","output_index":1,"delta":"\"qa.txt\" }"}),
        json!({"type":"response.output_item.done","output_index":1,"item":tool}),
        json!({"type":"response.completed","response":{"status":"completed","output":[reason,tool],"usage":{"input_tokens":20,"output_tokens":5,"input_tokens_details":{"cached_tokens":4}}}}),
    ];
    let wire = events
        .iter()
        .map(|e| format!("data: {e}\r\n\r\n"))
        .collect::<String>();
    let mut decoder = crate::bridge::stream::SseDecoder::default();
    let mut output = vec![translator.started()];
    for byte in wire.bytes() {
        for event in decoder.feed(&[byte]).unwrap() {
            output.extend(translator.accept_responses(event).unwrap());
        }
    }
    assert!(translator.finished);
    assert_eq!(output.last().unwrap()["type"], "message_stop");
    assert_eq!(translator.content[0]["signature"], signature(&reason));
    assert_eq!(translator.content[1]["input"]["file_path"], "qa.txt");
    let end = output
        .iter()
        .find(|e| e["type"] == "message_delta")
        .unwrap();
    assert_eq!(end["delta"]["stop_reason"], "tool_use");
    assert_eq!(end["usage"]["input_tokens"], 16);
    assert_eq!(end["usage"]["cache_read_input_tokens"], 4);
}

#[test]
fn chat_sse_handles_fragmented_tool_arguments_done_and_usage() {
    let mut translator = Translator::new("gpt-4.1");
    for event in [
        json!({"choices":[{"delta":{"content":"中文","tool_calls":[{"index":0,"id":"call1","function":{"name":"Read","arguments":"{\"file_path\":"}}]},"finish_reason":null}]}),
        json!({"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"qa.txt\"}"}}]},"finish_reason":"tool_calls"}]}),
        json!({"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2}}),
    ] {
        translator.accept_chat(event).unwrap();
    }
    let mut decoder = crate::bridge::stream::SseDecoder::default();
    let done = decoder.feed(b"data: [DONE]\n\n").unwrap().remove(0);
    let events = translator.accept_chat(done).unwrap();
    assert!(translator.finished);
    assert_eq!(translator.content[0]["text"], "中文");
    assert_eq!(translator.content[1]["name"], "Read");
    assert_eq!(events[events.len() - 2]["usage"]["output_tokens"], 2);
}

#[test]
fn nonstream_and_invalid_outputs_have_correct_stop_states() {
    let output=response(&json!({"status":"incomplete","output":[{"type":"message","content":[{"type":"output_text","text":"Partial"}]}]}),"gpt-5.4",false).unwrap();
    assert_eq!(output["stop_reason"], "max_tokens");
    let chat=response(&json!({"choices":[{"message":{"content":"Done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":9,"completion_tokens":2}}),"gpt-4.1",true).unwrap();
    assert_eq!(chat["stop_reason"], "end_turn");
    assert_eq!(chat["usage"]["input_tokens"], 9);
    for invalid in [
        json!({"status":"failed","output":[]}),
        json!({"status":"completed","output":[{"type":"function_call","call_id":"c1","name":"Read","arguments":"broken"}]}),
        json!({"status":"completed","output":[{"type":"function_call","arguments":"{}"}]}),
    ] {
        assert!(response(&invalid, "gpt", false).is_err());
    }
    assert!(Translator::new("gpt").finish_chat().is_err());
    assert!(Translator::new("gpt")
        .accept_responses(json!({"type":"response.failed"}))
        .is_err());
}

#[test]
fn old_claude_defaults_remain_native_and_model_alias_is_deterministic() {
    let options: crate::types::CodexOptions =
        serde_json::from_value(json!({"protocol":"openai"})).unwrap();
    assert_eq!(
        options.claude_protocol,
        crate::types::ClaudeProtocol::Anthropic
    );
    assert!(serde_json::to_value(&options)
        .unwrap()
        .get("claudeProtocol")
        .is_none());
    assert_eq!(model_alias("gpt-5.4"), model_alias("gpt-5.4"));
    assert_ne!(model_alias("gpt-5.4"), model_alias("gpt-4.1"));
    assert!(model_alias("gpt-5.4").starts_with("claude-sonnet-4-6-uni-"));
}

#[test]
fn reasoning_multiple_summary_parts_do_not_break_completed_stream() {
    let mut translator = Translator::new("gpt-5.4");
    for (part, text) in [(0, "First"), (1, "Second"), (1, " part")] {
        translator.accept_responses(json!({"type":"response.reasoning_summary_text.delta","output_index":0,"summary_index":part,"delta":text})).unwrap();
    }
    translator.accept_responses(json!({"type":"response.completed","response":{"status":"completed","output":[{"type":"reasoning","id":"r1","summary":[{"type":"summary_text","text":"First"},{"type":"summary_text","text":"Second part"}],"encrypted_content":"encrypted"}]}})).unwrap();
    assert!(translator.finished);
    assert_eq!(translator.content[0]["thinking"], "First\nSecond part");
}
