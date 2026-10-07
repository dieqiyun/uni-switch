use super::convert::ConversionResult;
use base64::{engine::general_purpose::STANDARD, Engine};
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use std::collections::{BTreeMap, BTreeSet};

const REASONING_PREFIX: &str = "uni-openai-reasoning:";

pub fn model_alias(model: &str) -> String {
    let digest = Sha256::digest(model.as_bytes());
    let hash: String = digest[..12].iter().map(|b| format!("{b:02x}")).collect();
    format!("claude-sonnet-4-6-uni-{hash}")
}
pub fn model_ids(provider: &crate::types::StoredProvider) -> Vec<String> {
    let mut ids = vec![provider.summary.model.clone()];
    for model in provider
        .summary
        .codex_options
        .models
        .iter()
        .filter(|m| m.enabled)
    {
        if !ids.contains(&model.id) {
            ids.push(model.id.clone());
        }
    }
    ids
}
pub fn resolve_model(
    provider: &crate::types::StoredProvider,
    requested: &str,
) -> ConversionResult<String> {
    let ids = model_ids(provider);
    if let Some(id) = ids
        .iter()
        .find(|id| id.as_str() == requested || model_alias(id) == requested)
    {
        return Ok(id.clone());
    }
    // Claude's internal summarization/Haiku requests use the applied default GPT.
    if matches!(requested, "sonnet" | "opus" | "haiku")
        || requested.starts_with("claude-") && !requested.contains("-uni-")
    {
        return Ok(provider.summary.model.clone());
    }
    Err("此 GPT 模型尚未启用，请同步并勾选后重新应用".into())
}
fn reasoning_model(model: &str) -> bool {
    let model = model.rsplit('/').next().unwrap_or(model);
    model.starts_with("gpt-5")
        || model.starts_with("gpt-6")
        || ["o1", "o3", "o4"].iter().any(|p| model.starts_with(p))
}
fn text_blocks(value: &Value, assistant: bool) -> ConversionResult<Vec<Value>> {
    if let Some(s) = value.as_str() {
        return Ok(vec![
            json!({"type":if assistant {"output_text"} else {"input_text"},"text":s}),
        ]);
    }
    let mut output = Vec::new();
    for block in value.as_array().ok_or("Claude 消息内容必须是文本或数组")? {
        match block["type"].as_str().unwrap_or("") {
            "text" => output.push(json!({"type":if assistant {"output_text"} else {"input_text"},"text":block["text"].as_str().ok_or("text 缺少文本")?})),
            "image" if !assistant => {
                let source = &block["source"];
                let url = match source["type"].as_str() {
                    Some("base64") => format!("data:{};base64,{}",source["media_type"].as_str().ok_or("图片缺少 media_type")?,source["data"].as_str().ok_or("图片缺少 data")?),
                    Some("url") => source["url"].as_str().ok_or("图片缺少 URL")?.to_owned(),
                    _ => return Err("此图片 source 类型尚未支持".into()),
                };
                output.push(json!({"type":"input_image","image_url":url}));
            }
            "tool_reference" => output.push(json!({"type":"input_text","text":format!("Available tool: {}",block["tool_name"].as_str().unwrap_or(""))})),
            other => return Err(format!("OpenAI 转换暂不支持 Claude 内容类型：{other}")),
        }
    }
    Ok(output)
}

pub fn request(body: &Value, model: &str, streaming: bool) -> ConversionResult<Value> {
    let mut input = Vec::new();
    let mut system = Vec::new();
    if let Some(value) = body.get("system") {
        for part in text_blocks(value, false)? {
            system.push(part["text"].as_str().ok_or("system 仅支持文本")?.to_owned());
        }
    }
    let messages = body["messages"]
        .as_array()
        .ok_or("Claude 请求缺少 messages")?;
    for message in messages {
        let role = message["role"].as_str().ok_or("消息缺少 role")?;
        if !matches!(role, "user" | "assistant") {
            return Err("Claude 消息 role 必须为 user 或 assistant".into());
        }
        let blocks = if message["content"].is_string() {
            vec![json!({"type":"text","text":message["content"]})]
        } else {
            message["content"]
                .as_array()
                .ok_or("消息缺少 content")?
                .clone()
        };
        let mut ordinary = Vec::new();
        let flush = |ordinary: &mut Vec<Value>, input: &mut Vec<Value>| {
            if !ordinary.is_empty() {
                input
                    .push(json!({"type":"message","role":role,"content":std::mem::take(ordinary)}));
            }
        };
        for block in blocks {
            match block["type"].as_str().unwrap_or("") {
                "tool_use" if role == "assistant" => {
                    flush(&mut ordinary, &mut input);
                    input.push(json!({"type":"function_call","call_id":block["id"].as_str().ok_or("tool_use 缺少 id")?,"name":block["name"].as_str().ok_or("tool_use 缺少 name")?,"arguments":block["input"].to_string()}));
                }
                "tool_result" if role == "user" => {
                    flush(&mut ordinary, &mut input);
                    let mut content =
                        text_blocks(block.get("content").unwrap_or(&json!("")), false)?;
                    if block["is_error"] == true {
                        content.insert(
                            0,
                            json!({"type":"input_text","text":"Tool execution failed:"}),
                        );
                    }
                    input.push(json!({"type":"function_call_output","call_id":block["tool_use_id"].as_str().ok_or("tool_result 缺少 tool_use_id")?,"output":content}));
                }
                "thinking" | "redacted_thinking" => {
                    flush(&mut ordinary, &mut input);
                    if let Some(original) = block["signature"]
                        .as_str()
                        .and_then(|s| s.strip_prefix(REASONING_PREFIX))
                        .and_then(|s| STANDARD.decode(s).ok())
                        .and_then(|b| serde_json::from_slice::<Value>(&b).ok())
                        .filter(|v| v["type"] == "reasoning")
                    {
                        input.push(original);
                    }
                }
                _ => ordinary.extend(text_blocks(&json!([block]), role == "assistant")?),
            }
        }
        flush(&mut ordinary, &mut input);
    }
    if input.is_empty() {
        return Err("请求没有可发送的消息".into());
    }
    let max_tokens = body["max_tokens"].as_u64().unwrap_or(8192);
    if max_tokens == 0 || max_tokens > 128_000 {
        return Err("max_tokens 必须在 1–128000 之间".into());
    }
    let mut result = json!({"model":model,"input":input,"max_output_tokens":max_tokens,"stream":streaming,"store":false,"parallel_tool_calls":false});
    if !system.is_empty() {
        result["instructions"] = json!(system.join("\n\n"));
    }
    let mut tools = Vec::new();
    if let Some(list) = body.get("tools") {
        for tool in list.as_array().ok_or("tools 必须是数组")? {
            let kind = tool["type"].as_str().unwrap_or("custom");
            // Deferred tools are sent eagerly. Provider-hosted search tools have
            // no OpenAI equivalent; their declarations are excluded, never run.
            if kind.starts_with("tool_search_tool_") {
                continue;
            }
            if !matches!(kind, "custom" | "function") {
                return Err(format!("OpenAI 转换不支持 Claude 托管工具：{kind}"));
            }
            let schema = tool.get("input_schema").ok_or("工具缺少 input_schema")?;
            tools.push(json!({"type":"function","name":tool["name"].as_str().ok_or("工具缺少 name")?,"description":tool["description"].as_str().unwrap_or(""),"parameters":schema,"strict":false}));
        }
    }
    if !tools.is_empty() {
        result["tools"] = json!(tools);
    }
    if let Some(choice) = body.get("tool_choice") {
        result["tool_choice"] = match choice["type"].as_str().unwrap_or("auto") {
            "auto" => json!("auto"),
            "any" => json!("required"),
            "none" => json!("none"),
            "tool" => {
                json!({"type":"function","name":choice["name"].as_str().ok_or("tool_choice 缺少 name")?})
            }
            _ => return Err("不支持此 tool_choice".into()),
        };
        result["parallel_tool_calls"] = json!(choice["disable_parallel_tool_use"] != true);
    }
    let thinking = matches!(
        body["thinking"]["type"].as_str(),
        Some("enabled" | "adaptive")
    );
    if thinking && reasoning_model(model) {
        let effort = body
            .pointer("/output_config/effort")
            .and_then(Value::as_str)
            .unwrap_or_else(
                || match body["thinking"]["budget_tokens"].as_u64().unwrap_or(4096) {
                    0..=1024 => "low",
                    1025..=3072 => "medium",
                    _ => "high",
                },
            );
        let effort = match effort {
            "low" | "medium" => effort,
            _ => "high",
        };
        result["reasoning"] = json!({"effort":effort,"summary":"auto"});
        result["include"] = json!(["reasoning.encrypted_content"]);
    }
    if !reasoning_model(model) {
        for field in ["temperature", "top_p"] {
            if let Some(value) = body.get(field) {
                result[field] = value.clone();
            }
        }
    }
    if let Some(schema) = body
        .pointer("/output_config/format")
        .filter(|f| f["type"] == "json_schema")
    {
        result["text"] = json!({"format":{"type":"json_schema","name":"claude_output","schema":schema["schema"],"strict":true}});
    }
    Ok(result)
}

pub fn chat_request(responses: &Value) -> ConversionResult<Value> {
    let mut messages = Vec::new();
    if let Some(s) = responses["instructions"].as_str() {
        messages.push(json!({"role":"system","content":s}));
    }
    for item in responses["input"]
        .as_array()
        .ok_or("Responses input 无效")?
    {
        match item["type"].as_str().unwrap_or("message") {
            "message" => {
                let mut parts = Vec::new();
                for part in item["content"].as_array().ok_or("消息 content 无效")? {
                    match part["type"].as_str() {
                        Some("input_text" | "output_text") => {
                            parts.push(json!({"type":"text","text":part["text"]}))
                        }
                        Some("input_image") => parts.push(
                            json!({"type":"image_url","image_url":{"url":part["image_url"]}}),
                        ),
                        _ => return Err("Chat Completions 不支持此消息内容".into()),
                    }
                }
                messages.push(json!({"role":item["role"],"content":parts}));
            }
            "function_call" => {
                let tool = json!({"id":item["call_id"],"type":"function","function":{"name":item["name"],"arguments":item["arguments"]}});
                if let Some(last) = messages
                    .last_mut()
                    .filter(|m| m["role"] == "assistant" && m.get("tool_calls").is_some())
                {
                    last["tool_calls"].as_array_mut().unwrap().push(tool);
                } else {
                    messages.push(json!({"role":"assistant","content":null,"tool_calls":[tool]}));
                }
            }
            "function_call_output" => {
                let mut parts = Vec::new();
                for part in item["output"].as_array().ok_or("工具结果 output 无效")? {
                    if part["type"] == "input_text" {
                        parts.push(json!({"type":"text","text":part["text"]}));
                    } else {
                        return Err("此 Chat Completions 接口不能在工具结果中接收图片，请使用 Responses 接口".into());
                    }
                }
                messages
                    .push(json!({"role":"tool","tool_call_id":item["call_id"],"content":parts}));
            }
            "reasoning" => {}
            _ => return Err("Chat Completions 暂不支持此 input 类型".into()),
        }
    }
    let mut result =
        json!({"model":responses["model"],"messages":messages,"stream":responses["stream"]});
    let field = if reasoning_model(responses["model"].as_str().unwrap_or("")) {
        "max_completion_tokens"
    } else {
        "max_tokens"
    };
    result[field] = responses["max_output_tokens"].clone();
    if responses["stream"] == true {
        result["stream_options"] = json!({"include_usage":true});
    }
    if let Some(list) = responses["tools"].as_array() {
        result["tools"] = json!(list.iter().map(|t|json!({"type":"function","function":{"name":t["name"],"description":t["description"],"parameters":t["parameters"],"strict":false}})).collect::<Vec<_>>());
        result["parallel_tool_calls"] = responses["parallel_tool_calls"].clone();
    }
    if let Some(choice) = responses.get("tool_choice") {
        result["tool_choice"] = if choice.is_string() {
            choice.clone()
        } else {
            json!({"type":"function","function":{"name":choice["name"]}})
        };
    }
    if let Some(effort) = responses.pointer("/reasoning/effort") {
        result["reasoning_effort"] = effort.clone();
    }
    for field in ["temperature", "top_p"] {
        if let Some(v) = responses.get(field) {
            result[field] = v.clone();
        }
    }
    if let Some(format) = responses.pointer("/text/format") {
        result["response_format"] = json!({"type":"json_schema","json_schema":{"name":format["name"],"schema":format["schema"],"strict":true}});
    }
    Ok(result)
}
pub fn usage(value: &Value, chat: bool) -> Value {
    let input = value[if chat {
        "prompt_tokens"
    } else {
        "input_tokens"
    }]
    .as_u64()
    .unwrap_or(0);
    let cached = value
        .pointer(if chat {
            "/prompt_tokens_details/cached_tokens"
        } else {
            "/input_tokens_details/cached_tokens"
        })
        .and_then(Value::as_u64)
        .unwrap_or(0)
        .min(input);
    let output = value[if chat {
        "completion_tokens"
    } else {
        "output_tokens"
    }]
    .as_u64()
    .unwrap_or(0);
    json!({"input_tokens":input-cached,"output_tokens":output,"cache_creation_input_tokens":0,"cache_read_input_tokens":cached})
}
pub fn signature(item: &Value) -> String {
    format!("{REASONING_PREFIX}{}", STANDARD.encode(item.to_string()))
}
pub fn response(body: &Value, model: &str, chat: bool) -> ConversionResult<Value> {
    let mut content = Vec::new();
    let mut stop = "end_turn";
    let tokens = if chat {
        let choice = body["choices"]
            .as_array()
            .and_then(|a| a.first())
            .ok_or("OpenAI 回复缺少 choices")?;
        let message = &choice["message"];
        if let Some(t) = message["reasoning_content"]
            .as_str()
            .filter(|s| !s.is_empty())
        {
            content.push(json!({"type":"thinking","thinking":t,"signature":""}));
        }
        if let Some(t) = message["content"].as_str().filter(|s| !s.is_empty()) {
            content.push(json!({"type":"text","text":t}));
        }
        if let Some(t) = message["refusal"].as_str() {
            content.push(json!({"type":"text","text":t}));
        }
        if let Some(tools) = message["tool_calls"].as_array() {
            for tool in tools {
                let input: Value = serde_json::from_str(
                    tool["function"]["arguments"]
                        .as_str()
                        .ok_or("工具调用缺少 arguments")?,
                )
                .map_err(|_| "OpenAI 返回了无效工具 JSON")?;
                validate_tool(&tool["id"], &tool["function"]["name"], &input)?;
                content.push(json!({"type":"tool_use","id":tool["id"],"name":tool["function"]["name"],"input":input}));
            }
        }
        match choice["finish_reason"].as_str() {
            Some("length") => stop = "max_tokens",
            Some("tool_calls" | "function_call") => stop = "tool_use",
            Some("content_filter") => stop = "refusal",
            Some("stop") => {}
            _ => return Err("OpenAI 回复缺少完整结束状态".into()),
        };
        usage(&body["usage"], true)
    } else {
        if !matches!(body["status"].as_str(), Some("completed" | "incomplete")) {
            return Err("OpenAI 回复未完整结束".into());
        }
        if body["status"] == "incomplete" {
            stop = "max_tokens";
        }
        for item in body["output"].as_array().ok_or("OpenAI 回复缺少 output")? {
            content.extend(response_item(item)?);
        }
        usage(&body["usage"], false)
    };
    if stop != "max_tokens" && content.iter().any(|b| b["type"] == "tool_use") {
        stop = "tool_use";
    }
    Ok(
        json!({"id":format!("msg_{}",uuid::Uuid::new_v4().simple()),"type":"message","role":"assistant","model":model,"content":content,"stop_reason":stop,"stop_sequence":null,"usage":tokens}),
    )
}
fn response_item(item: &Value) -> ConversionResult<Vec<Value>> {
    match item["type"].as_str().unwrap_or("") {
        "message" => item["content"]
            .as_array()
            .ok_or("OpenAI 消息缺少 content")?
            .iter()
            .map(|p| {
                let text = match p["type"].as_str() {
                    Some("output_text") => p["text"].as_str(),
                    Some("refusal") => p["refusal"].as_str(),
                    _ => None,
                }
                .ok_or("OpenAI 返回了不支持的内容类型")?;
                Ok(json!({"type":"text","text":text}))
            })
            .collect(),
        "function_call" => {
            let input: Value =
                serde_json::from_str(item["arguments"].as_str().ok_or("工具缺少 arguments")?)
                    .map_err(|_| "OpenAI 返回了无效工具 JSON")?;
            validate_tool(&item["call_id"], &item["name"], &input)?;
            Ok(vec![
                json!({"type":"tool_use","id":item["call_id"],"name":item["name"],"input":input}),
            ])
        }
        "reasoning" => {
            let thinking = item["summary"]
                .as_array()
                .map(|a| {
                    a.iter()
                        .filter_map(|p| p["text"].as_str())
                        .collect::<Vec<_>>()
                        .join("\n")
                })
                .unwrap_or_default();
            Ok(vec![
                json!({"type":"thinking","thinking":thinking,"signature":signature(item)}),
            ])
        }
        other => Err(format!("OpenAI 返回了不支持的输出类型：{other}")),
    }
}
fn validate_tool(id: &Value, name: &Value, input: &Value) -> ConversionResult<()> {
    if !id.as_str().is_some_and(|s| !s.is_empty())
        || !name.as_str().is_some_and(|s| !s.is_empty())
        || !input.is_object()
    {
        return Err("OpenAI 工具调用缺少有效 id、name 或对象参数".into());
    }
    Ok(())
}

#[derive(Default)]
struct Block {
    index: usize,
    content: Value,
    raw: String,
    closed: bool,
}
pub struct Translator {
    model: String,
    blocks: BTreeMap<String, Block>,
    pub finished: bool,
    chat_finish: Option<String>,
    tokens: Value,
    chat_tools: BTreeMap<u64, Value>,
    summary_parts: BTreeSet<(u64, u64)>,
    pub content: Vec<Value>,
}
impl Translator {
    pub fn new(model: &str) -> Self {
        Self {
            model: model.into(),
            blocks: BTreeMap::new(),
            finished: false,
            chat_finish: None,
            tokens: usage(&Value::Null, false),
            chat_tools: BTreeMap::new(),
            summary_parts: BTreeSet::new(),
            content: vec![],
        }
    }
    pub fn started(&self) -> Value {
        json!({"type":"message_start","message":{"id":format!("msg_{}",uuid::Uuid::new_v4().simple()),"type":"message","role":"assistant","model":self.model,"content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}})
    }
    fn start(&mut self, key: &str, content: Value, events: &mut Vec<Value>) {
        if !self.blocks.contains_key(key) {
            let index = self.blocks.len();
            events
                .push(json!({"type":"content_block_start","index":index,"content_block":content}));
            self.blocks.insert(
                key.into(),
                Block {
                    index,
                    content,
                    raw: String::new(),
                    closed: false,
                },
            );
        }
    }
    fn delta(
        &mut self,
        key: &str,
        delta: &str,
        kind: &str,
        events: &mut Vec<Value>,
    ) -> ConversionResult<()> {
        let block = self
            .blocks
            .get_mut(key)
            .ok_or("OpenAI delta 缺少内容开始")?;
        if block.closed {
            return Err("OpenAI 内容结束后仍发送 delta".into());
        }
        block.raw.push_str(delta);
        let field = match kind {
            "text_delta" => "text",
            "thinking_delta" => "thinking",
            _ => "partial_json",
        };
        let mut value = json!({"type":kind});
        value[field] = json!(delta);
        events.push(json!({"type":"content_block_delta","index":block.index,"delta":value}));
        Ok(())
    }
    fn close(&mut self, key: &str, events: &mut Vec<Value>) -> ConversionResult<()> {
        if let Some(block) = self.blocks.get_mut(key).filter(|b| !b.closed) {
            match block.content["type"].as_str() {
                Some("tool_use") => {
                    block.content["input"] = serde_json::from_str(&block.raw)
                        .map_err(|_| "OpenAI 返回了无效工具 JSON")?
                }
                Some("text") => block.content["text"] = json!(block.raw),
                Some("thinking") => block.content["thinking"] = json!(block.raw),
                _ => {}
            }
            block.closed = true;
            events.push(json!({"type":"content_block_stop","index":block.index}));
        }
        Ok(())
    }
    fn whole(
        &mut self,
        key: &str,
        block: Value,
        raw_override: Option<&str>,
        events: &mut Vec<Value>,
    ) -> ConversionResult<()> {
        let kind = block["type"].as_str().ok_or("输出缺少 type")?;
        let raw = match kind {
            "text" => block["text"].as_str().unwrap_or("").to_owned(),
            "thinking" => block["thinking"].as_str().unwrap_or("").to_owned(),
            "tool_use" => raw_override
                .map(str::to_owned)
                .unwrap_or_else(|| block["input"].to_string()),
            _ => return Err("不支持的输出块".into()),
        };
        if kind == "tool_use" {
            validate_tool(&block["id"], &block["name"], &block["input"])?;
        }
        let mut initial = block.clone();
        let delta_kind = match kind {
            "text" => {
                initial["text"] = json!("");
                "text_delta"
            }
            "thinking" => {
                initial["thinking"] = json!("");
                initial["signature"] = json!("");
                "thinking_delta"
            }
            _ => {
                initial["input"] = json!({});
                "input_json_delta"
            }
        };
        self.start(key, initial, events);
        let old = self.blocks[key].raw.clone();
        if !self.blocks[key].closed {
            let remaining = raw
                .strip_prefix(&old)
                .ok_or("OpenAI 完成内容与已发送增量不一致")?;
            if !remaining.is_empty() {
                self.delta(key, remaining, delta_kind, events)?;
            }
            if kind == "thinking" {
                let signature = block["signature"].as_str().unwrap_or("");
                self.blocks.get_mut(key).unwrap().content["signature"] = json!(signature);
                if !signature.is_empty() {
                    events.push(json!({"type":"content_block_delta","index":self.blocks[key].index,"delta":{"type":"signature_delta","signature":signature}}));
                }
            }
            self.close(key, events)?;
        }
        Ok(())
    }
    fn complete(&mut self, stop: &str, events: &mut Vec<Value>) -> ConversionResult<()> {
        let keys = self.blocks.keys().cloned().collect::<Vec<_>>();
        for key in keys {
            self.close(&key, events)?;
        }
        let mut sorted = self.blocks.values().collect::<Vec<_>>();
        sorted.sort_by_key(|b| b.index);
        self.content = sorted.into_iter().map(|b| b.content.clone()).collect();
        let stop = if stop == "end_turn" && self.content.iter().any(|c| c["type"] == "tool_use") {
            "tool_use"
        } else {
            stop
        };
        events.push(json!({"type":"message_delta","delta":{"stop_reason":stop,"stop_sequence":null},"usage":self.tokens}));
        events.push(json!({"type":"message_stop"}));
        self.finished = true;
        Ok(())
    }
    pub fn accept_responses(&mut self, event: Value) -> ConversionResult<Vec<Value>> {
        let mut events = Vec::new();
        let index = event["output_index"].as_u64().unwrap_or(0);
        let part = event["content_index"].as_u64().unwrap_or(0);
        let kind = event["type"].as_str().unwrap_or("");
        match kind {
            "response.output_item.added" if event["item"]["type"]=="function_call" => self.start(&format!("tool:{index}"),json!({"type":"tool_use","id":event["item"]["call_id"],"name":event["item"]["name"],"input":{}}),&mut events),
            "response.output_text.delta" | "response.refusal.delta" => {
                let key=format!("text:{index}:{part}");self.start(&key,json!({"type":"text","text":""}),&mut events);
                self.delta(&key,event["delta"].as_str().ok_or("文本 delta 无效")?,"text_delta",&mut events)?;
            }
            "response.function_call_arguments.delta" => self.delta(&format!("tool:{index}"),event["delta"].as_str().ok_or("工具 delta 无效")?,"input_json_delta",&mut events)?,
            "response.reasoning_summary_text.delta" => {
                let key=format!("reasoning:{index}");self.start(&key,json!({"type":"thinking","thinking":"","signature":""}),&mut events);
                let summary_index=event["summary_index"].as_u64().unwrap_or(0);
                if self.summary_parts.insert((index,summary_index)) && summary_index>0 && !self.blocks[&key].raw.is_empty() {
                    self.delta(&key,"\n","thinking_delta",&mut events)?;
                }
                self.delta(&key,event["delta"].as_str().ok_or("思考 delta 无效")?,"thinking_delta",&mut events)?;
            }
            "response.output_item.done" => self.final_item(index,&event["item"],&mut events)?,
            "response.completed" | "response.incomplete" => {
                let response=&event["response"];
                if response["status"]=="failed" {return Err("OpenAI 回复失败".into());}
                if let Some(output)=response["output"].as_array(){for(index,item)in output.iter().enumerate(){self.final_item(index as u64,item,&mut events)?;}}
                self.tokens=usage(&response["usage"],false);
                let stop=if kind=="response.incomplete" || response["status"]=="incomplete"{"max_tokens"}else{"end_turn"};
                self.complete(stop,&mut events)?;
            }
            "response.failed" | "error" => return Err(event.pointer("/response/error/message").or_else(||event.pointer("/error/message")).or_else(||event.get("message")).and_then(Value::as_str).unwrap_or("OpenAI 流式回复失败").into()),
            other if other.starts_with("response.") => {},
            _ => return Err("OpenAI 返回了未知流式事件".into()),
        }
        Ok(events)
    }
    fn final_item(
        &mut self,
        index: u64,
        item: &Value,
        events: &mut Vec<Value>,
    ) -> ConversionResult<()> {
        for (part, block) in response_item(item)?.into_iter().enumerate() {
            let key = match block["type"].as_str() {
                Some("tool_use") => format!("tool:{index}"),
                Some("thinking") => format!("reasoning:{index}"),
                _ => format!("text:{index}:{part}"),
            };
            let raw = if block["type"] == "tool_use" {
                item["arguments"].as_str()
            } else {
                None
            };
            self.whole(&key, block, raw, events)?;
        }
        Ok(())
    }
    pub fn accept_chat(&mut self, event: Value) -> ConversionResult<Vec<Value>> {
        let mut events = Vec::new();
        if event["type"] == "uni_switch_done" {
            return self.finish_chat();
        }
        if event.get("error").is_some() {
            return Err(event["error"]["message"]
                .as_str()
                .unwrap_or("OpenAI 流式错误")
                .into());
        }
        if event.get("usage").is_some_and(|u| u.is_object()) {
            self.tokens = usage(&event["usage"], true);
        }
        if let Some(choice) = event["choices"].as_array().and_then(|a| a.first()) {
            let delta = &choice["delta"];
            for (field, key, kind) in [
                ("content", "chat:text", "text_delta"),
                ("reasoning_content", "chat:thinking", "thinking_delta"),
            ] {
                if let Some(text) = delta[field].as_str().filter(|s| !s.is_empty()) {
                    let content = if field == "content" {
                        json!({"type":"text","text":""})
                    } else {
                        json!({"type":"thinking","thinking":"","signature":""})
                    };
                    self.start(key, content, &mut events);
                    self.delta(key, text, kind, &mut events)?;
                }
            }
            if let Some(list) = delta["tool_calls"].as_array() {
                for tool in list {
                    let index = tool["index"].as_u64().ok_or("Chat 工具缺少 index")?;
                    let saved = self
                        .chat_tools
                        .entry(index)
                        .or_insert_with(|| json!({"id":"","name":"","arguments":""}));
                    for (field, value) in [
                        ("id", tool.get("id")),
                        ("name", tool.pointer("/function/name")),
                        ("arguments", tool.pointer("/function/arguments")),
                    ] {
                        if let Some(s) = value.and_then(Value::as_str) {
                            saved[field] =
                                json!(format!("{}{s}", saved[field].as_str().unwrap_or("")));
                        }
                    }
                }
            }
            if let Some(finish) = choice["finish_reason"].as_str() {
                self.chat_finish = Some(finish.into());
            }
        }
        Ok(events)
    }
    pub fn finish_chat(&mut self) -> ConversionResult<Vec<Value>> {
        let finish = self
            .chat_finish
            .clone()
            .ok_or("OpenAI Chat 流在结束标记前断开")?;
        let mut events = Vec::new();
        for (index, tool) in self.chat_tools.clone() {
            let input: Value = serde_json::from_str(tool["arguments"].as_str().unwrap_or(""))
                .map_err(|_| "OpenAI 返回了无效工具 JSON")?;
            self.whole(
                &format!("chat:tool:{index}"),
                json!({"type":"tool_use","id":tool["id"],"name":tool["name"],"input":input}),
                tool["arguments"].as_str(),
                &mut events,
            )?;
        }
        let stop = match finish.as_str() {
            "stop" => "end_turn",
            "length" => "max_tokens",
            "tool_calls" | "function_call" => "tool_use",
            "content_filter" => "refusal",
            _ => return Err("Chat 返回了未知结束原因".into()),
        };
        self.complete(stop, &mut events)?;
        Ok(events)
    }
}

#[cfg(test)]
mod tests;
