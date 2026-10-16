use super::convert::{self, ConversionResult, Tool};
use serde_json::{json, Value};
use std::collections::BTreeMap;

// SSE is decoded by complete byte lines: a network chunk may split a UTF-8 character.
const MAX_EVENT_BYTES: usize = 4 * 1024 * 1024;

#[derive(Default)]
pub struct SseDecoder {
    buffer: Vec<u8>,
    data: Vec<String>,
    data_bytes: usize,
    skip_lf: bool,
    started: bool,
}
impl SseDecoder {
    pub fn finish(&self) -> ConversionResult<()> {
        if !self.buffer.is_empty() || !self.data.is_empty() {
            return Err("流式连接在事件完整结束前断开".into());
        }
        Ok(())
    }
    pub fn feed(&mut self, bytes: &[u8]) -> ConversionResult<Vec<Value>> {
        let mut events = Vec::new();
        // Bound each unfinished line/event instead of a complete network chunk:
        // one chunk may contain many small events, while one event may span chunks.
        let mut remaining = bytes;
        while !remaining.is_empty() {
            if self.skip_lf {
                self.skip_lf = false;
                if remaining[0] == b'\n' {
                    remaining = &remaining[1..];
                    continue;
                }
            }
            let end = remaining.iter().position(|b| matches!(b, b'\r' | b'\n'));
            let length = end.unwrap_or(remaining.len());
            if self.buffer.len() + length + self.data_bytes > MAX_EVENT_BYTES {
                return Err("流式事件超过 4 MiB 大小限制".into());
            }
            self.buffer.extend_from_slice(&remaining[..length]);
            let Some(end) = end else { break };
            self.skip_lf = remaining[end] == b'\r';
            remaining = &remaining[end + 1..];
            let mut line =
                std::str::from_utf8(&self.buffer).map_err(|_| "流式事件包含无效 UTF-8")?;
            if !self.started {
                line = line.strip_prefix('\u{feff}').unwrap_or(line);
                self.started = true;
            }
            if line.is_empty() {
                if !self.data.is_empty() {
                    let data = self.data.join("\n");
                    events.push(if data.trim() == "[DONE]" {
                        json!({"type":"uni_switch_done"})
                    } else {
                        serde_json::from_str(&data).map_err(|_| "流式事件不是有效 JSON")?
                    });
                    self.data.clear();
                    self.data_bytes = 0;
                }
            } else if let Some(data) = line.strip_prefix("data:") {
                let data = data.strip_prefix(' ').unwrap_or(data);
                self.data_bytes += data.len() + 1;
                if self.data_bytes > MAX_EVENT_BYTES {
                    return Err("流式事件超过 4 MiB 大小限制".into());
                }
                self.data.push(data.to_owned());
            }
            self.buffer.clear();
        }
        Ok(events)
    }
}

struct Block {
    id: String,
    output_index: usize,
    value: Value,
    arguments: String,
    summary_started: bool,
    closed: bool,
}
pub struct Translator {
    response: Value,
    tools: Vec<Tool>,
    blocks: BTreeMap<u64, Block>,
    tokens: Value,
    stop: String,
    pub finished: bool,
    sequence: u64,
    continuation: Option<Value>,
}
impl Translator {
    pub fn new(model: &str, tools: Vec<Tool>) -> Self {
        let mut response = convert::response(model, vec![], &json!({}), "");
        response["status"] = json!("in_progress");
        response["usage"] = Value::Null;
        Self {
            response,
            tools,
            blocks: BTreeMap::new(),
            tokens: json!({}),
            stop: String::new(),
            finished: false,
            sequence: 0,
            continuation: None,
        }
    }
    pub fn with_continuation(mut self, enabled: bool) -> Self {
        self.continuation = enabled.then(convert::continuation_item);
        self
    }
    fn event(&mut self, mut event: Value) -> Value {
        event["sequence_number"] = json!(self.sequence);
        self.sequence += 1;
        event
    }
    pub fn started(&mut self) -> Vec<Value> {
        let mut events = vec![
            self.event(json!({"type":"response.created","response":self.response})),
            self.event(json!({"type":"response.in_progress","response":self.response})),
        ];
        if let Some(item) = self.continuation.clone() {
            events.push(
                self.event(
                    json!({"type":"response.output_item.added","output_index":0,"item":item}),
                ),
            );
            events.push(
                self.event(
                    json!({"type":"response.output_item.done","output_index":0,"item":item}),
                ),
            );
        }
        events
    }
    pub fn accept(&mut self, event: Value) -> ConversionResult<Vec<Value>> {
        let mut events = Vec::new();
        match event["type"].as_str().unwrap_or("") {
            "message_start" => {
                self.tokens = event["message"]["usage"].clone();
                if !self.tokens.is_object() {
                    self.tokens = json!({});
                }
            }
            "content_block_start" => {
                let index = event["index"].as_u64().ok_or("Claude 流缺少内容索引")?;
                if self.blocks.contains_key(&index) {
                    return Err("Claude 流重复内容索引".into());
                }
                let value = event["content_block"].clone();
                let summary_started = value["type"] == "thinking"
                    && value["thinking"]
                        .as_str()
                        .is_some_and(|text| !text.is_empty());
                let id = format!("item_{}", uuid::Uuid::new_v4().simple());
                let output_index = self.blocks.len() + usize::from(self.continuation.is_some());
                let mut item = convert::item(&value, &self.tools, &id).or_else(|e| {
                    // Custom tool input normally starts as {} and arrives in deltas.
                    if value["type"] == "tool_use" {
                        let mut placeholder = value.clone();
                        placeholder["input"] = json!({"input":""});
                        convert::item(&placeholder, &self.tools, &id)
                    } else {
                        Err(e)
                    }
                })?;
                item["status"] = json!("in_progress");
                if item["type"] == "message" {
                    item["content"] = json!([]);
                }
                if item["type"] == "function_call" {
                    item["arguments"] = json!("");
                }
                if item["type"] == "custom_tool_call" {
                    item["input"] = json!("");
                }
                if item["type"] == "reasoning" {
                    item.as_object_mut().unwrap().remove("encrypted_content");
                    item["summary"] = json!([]);
                }
                events.push(json!({"type":"response.output_item.added","output_index":output_index,"item":item}));
                match value["type"].as_str() {
                    Some("text") => {
                        events.push(json!({"type":"response.content_part.added","item_id":id,"output_index":output_index,"content_index":0,"part":{"type":"output_text","text":"","annotations":[]}}));
                        if let Some(s) = value["text"].as_str().filter(|s| !s.is_empty()) {
                            events.push(json!({"type":"response.output_text.delta","item_id":id,"output_index":output_index,"content_index":0,"delta":s}));
                        }
                    }
                    Some("thinking") if summary_started => {
                        events.push(json!({"type":"response.reasoning_summary_part.added","item_id":id,"output_index":output_index,"summary_index":0,"part":{"type":"summary_text","text":""}}));
                        events.push(json!({"type":"response.reasoning_summary_text.delta","item_id":id,"output_index":output_index,"summary_index":0,"delta":value["thinking"]}));
                    }
                    _ => {}
                }
                self.blocks.insert(
                    index,
                    Block {
                        id,
                        output_index,
                        value,
                        arguments: String::new(),
                        summary_started,
                        closed: false,
                    },
                );
            }
            "content_block_delta" => {
                let block = self
                    .blocks
                    .get_mut(&event["index"].as_u64().ok_or("Claude 流缺少内容索引")?)
                    .ok_or("Claude 流缺少 content_block_start")?;
                if block.closed {
                    return Err("Claude 流在内容结束后返回 delta".into());
                }
                let delta = &event["delta"];
                let (field, value, event_type) = match delta["type"].as_str().unwrap_or("") {
                    "text_delta" => ("text", "text", "response.output_text.delta"),
                    "thinking_delta" => (
                        "thinking",
                        "thinking",
                        "response.reasoning_summary_text.delta",
                    ),
                    "signature_delta" => ("signature", "signature", ""),
                    "input_json_delta" => (
                        "arguments",
                        "partial_json",
                        "response.function_call_arguments.delta",
                    ),
                    other => return Err(format!("不支持的 Claude delta：{other}")),
                };
                let addition = delta[value].as_str().ok_or("Claude delta 缺少文本")?;
                if field == "arguments" {
                    block.arguments.push_str(addition);
                } else if !addition.is_empty() || block.value.get(field).is_some() {
                    let mut s = block.value[field].as_str().unwrap_or("").to_owned();
                    s.push_str(addition);
                    block.value[field] = json!(s);
                }
                let custom = block.value["type"] == "tool_use"
                    && self
                        .tools
                        .iter()
                        .any(|t| t.upstream == block.value["name"] && t.custom);
                if field == "thinking" && !block.summary_started && !addition.is_empty() {
                    events.push(json!({"type":"response.reasoning_summary_part.added","item_id":block.id,"output_index":block.output_index,"summary_index":0,"part":{"type":"summary_text","text":""}}));
                    block.summary_started = true;
                }
                if !event_type.is_empty()
                    && !custom
                    && (field != "thinking" || block.summary_started)
                {
                    let mut output = json!({"type":event_type,"item_id":block.id,"output_index":block.output_index,"delta":addition});
                    if field == "thinking" {
                        output["summary_index"] = json!(0);
                    }
                    if field == "text" {
                        output["content_index"] = json!(0);
                    }
                    events.push(output);
                }
            }
            "content_block_stop" => {
                let block = self
                    .blocks
                    .get_mut(&event["index"].as_u64().ok_or("Claude 流缺少内容索引")?)
                    .ok_or("Claude 流缺少内容开始")?;
                if block.closed {
                    return Err("Claude 流重复内容结束".into());
                }
                if !block.arguments.is_empty() {
                    block.value["input"] = serde_json::from_str(&block.arguments)
                        .map_err(|_| "Claude 返回了无效工具 JSON")?;
                }
                let mut item = convert::item(&block.value, &self.tools, &block.id)?;
                match item["type"].as_str() {
                    Some("message") => {
                        events.push(json!({"type":"response.output_text.done","item_id":block.id,"output_index":block.output_index,"content_index":0,"text":block.value["text"]}));
                        events.push(json!({"type":"response.content_part.done","item_id":block.id,"output_index":block.output_index,"content_index":0,"part":item["content"][0]}));
                    }
                    Some("function_call") => events.push(json!({"type":"response.function_call_arguments.done","item_id":block.id,"output_index":block.output_index,"arguments":item["arguments"]})),
                    Some("custom_tool_call") => {
                        events.push(json!({"type":"response.custom_tool_call_input.delta","item_id":block.id,"output_index":block.output_index,"delta":item["input"]}));
                        events.push(json!({"type":"response.custom_tool_call_input.done","item_id":block.id,"output_index":block.output_index,"input":item["input"]}));
                    }
                    Some("reasoning") if block.value["type"] == "thinking" && block.summary_started => {
                        item["summary"] = json!([{"type":"summary_text","text":block.value["thinking"]}]);
                        events.push(json!({"type":"response.reasoning_summary_text.done","item_id":block.id,"output_index":block.output_index,"summary_index":0,"text":block.value["thinking"]}));
                        events.push(json!({"type":"response.reasoning_summary_part.done","item_id":block.id,"output_index":block.output_index,"summary_index":0,"part":item["summary"][0]}));
                    }
                    _ => {}
                }
                block.closed = true;
                events.push(json!({"type":"response.output_item.done","output_index":block.output_index,"item":item}));
            }
            "message_delta" => {
                if let Some(stop) = event["delta"]["stop_reason"].as_str() {
                    self.stop = stop.to_owned();
                }
                if let Some(tokens) = event["usage"].as_object() {
                    for (k, v) in tokens {
                        self.tokens[k] = v.clone();
                    }
                }
            }
            "message_stop" => {
                if self.blocks.values().any(|b| !b.closed) || self.stop.is_empty() {
                    return Err("Claude 流在内容完整结束前断开".into());
                }
                let mut output = self
                    .blocks
                    .values()
                    .map(|b| convert::item(&b.value, &self.tools, &b.id))
                    .collect::<ConversionResult<Vec<_>>>()?;
                if let Some(item) = &self.continuation {
                    output.insert(0, item.clone());
                }
                self.response["output"] = json!(output);
                self.response["usage"] = convert::usage(&self.tokens);
                self.response["status"] = json!(if self.stop == "max_tokens" {
                    "incomplete"
                } else {
                    "completed"
                });
                if self.stop == "max_tokens" {
                    self.response["incomplete_details"] = json!({"reason":"max_output_tokens"});
                }
                events.push(json!({"type":if self.stop == "max_tokens" {"response.incomplete"} else {"response.completed"},"response":self.response}));
                self.finished = true;
            }
            "error" => {
                return Err(event["error"]["message"]
                    .as_str()
                    .unwrap_or("Claude 返回流式错误")
                    .to_owned())
            }
            "ping" => {}
            other => return Err(format!("不支持的 Claude 流式事件：{other}")),
        }
        Ok(events.into_iter().map(|e| self.event(e)).collect())
    }
}
