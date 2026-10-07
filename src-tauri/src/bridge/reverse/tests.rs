use super::*;

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
