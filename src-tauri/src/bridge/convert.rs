use base64::{engine::general_purpose::STANDARD, Engine};
use serde_json::{json, Value};

pub type ConversionResult<T> = std::result::Result<T, String>;
const THINKING_PREFIX: &str = "uni-anthropic-thinking:";
const COMPACT_PREFIX: &str = "uni-anthropic-compaction:";

#[derive(Clone, Debug)]
pub struct Tool {
    pub upstream: String,
    pub name: String,
    pub namespace: Option<String>,
    pub custom: bool,
}

pub struct Converted {
    pub body: Value,
    pub tools: Vec<Tool>,
}

pub fn opaque_thinking(block: &Value) -> String {
    format!("{THINKING_PREFIX}{}", STANDARD.encode(block.to_string()))
}
pub fn opaque_compaction(text: &str) -> String {
    format!("{COMPACT_PREFIX}{}", STANDARD.encode(text))
}
fn decode(value: &str, prefix: &str) -> Option<String> {
    String::from_utf8(STANDARD.decode(value.strip_prefix(prefix)?).ok()?).ok()
}
fn text(value: &Value) -> ConversionResult<Vec<Value>> {
    if let Some(s) = value.as_str() {
        return Ok(if s.is_empty() {
            vec![]
        } else {
            vec![json!({"type":"text","text":s})]
        });
    }
    let mut blocks = Vec::new();
    for part in value.as_array().ok_or("消息 content 必须是文本或数组")? {
        match part["type"].as_str().unwrap_or("") {
            "input_text" | "output_text" | "text" => {
                if let Some(s) = part["text"].as_str().filter(|s| !s.is_empty()) {
                    blocks.push(json!({"type":"text","text":s}));
                }
            }
            "input_image" => {
                let url = part["image_url"].as_str().ok_or("图片缺少 image_url")?;
                let source = if let Some(data) = url.strip_prefix("data:") {
                    let (mime, data) = data
                        .split_once(";base64,")
                        .ok_or("图片必须使用 base64 data URL")?;
                    if !matches!(
                        mime,
                        "image/jpeg" | "image/png" | "image/gif" | "image/webp"
                    ) {
                        return Err("Claude 不支持此图片格式".into());
                    }
                    json!({"type":"base64","media_type":mime,"data":data})
                } else if url.starts_with("https://") || url.starts_with("http://") {
                    json!({"type":"url","url":url})
                } else {
                    return Err("图片必须使用 http(s) URL 或 base64 data URL".into());
                };
                blocks.push(json!({"type":"image","source":source}));
            }
            other => return Err(format!("协议转换暂不支持消息内容类型：{other}")),
        }
    }
    Ok(blocks)
}
fn push(messages: &mut Vec<Value>, role: &str, mut blocks: Vec<Value>) {
    if blocks.is_empty() {
        return;
    }
    if let Some(last) = messages.last_mut().filter(|m| m["role"] == role) {
        last["content"].as_array_mut().unwrap().append(&mut blocks);
    } else {
        messages.push(json!({"role":role,"content":blocks}));
    }
}
fn tools(request: &Value) -> ConversionResult<(Vec<Value>, Vec<Tool>)> {
    fn add(
        t: &Value,
        namespace: Option<&str>,
        schemas: &mut Vec<Value>,
        mapping: &mut Vec<Tool>,
    ) -> ConversionResult<()> {
        let kind = t["type"].as_str().unwrap_or("");
        if kind == "namespace" {
            let name = t["name"].as_str().ok_or("工具 namespace 缺少名称")?;
            for child in t["tools"].as_array().ok_or("工具 namespace 缺少 tools")? {
                add(child, Some(name), schemas, mapping)?;
            }
            return Ok(());
        }
        if !matches!(kind, "function" | "custom") {
            return Err(format!(
                "Claude 转换不支持 OpenAI 托管工具：{kind}，请使用 Codex 本地工具"
            ));
        }
        let name = t["name"].as_str().ok_or("工具缺少 name")?.to_owned();
        let candidate = namespace
            .map(|n| format!("{n}__{name}"))
            .unwrap_or_else(|| name.clone());
        let upstream = if candidate.len() <= 64
            && candidate
                .bytes()
                .all(|b| b.is_ascii_alphanumeric() || matches!(b, b'_' | b'-'))
            && !mapping.iter().any(|m| m.upstream == candidate)
        {
            candidate
        } else {
            format!("uni_tool_{}", mapping.len())
        };
        let custom = kind == "custom";
        let parameters = if custom {
            json!({"type":"object","properties":{"input":{"type":"string","description":"The complete raw tool input, following the tool's documented format."}},"required":["input"],"additionalProperties":false})
        } else {
            t.get("parameters")
                .cloned()
                .unwrap_or_else(|| json!({"type":"object","properties":{}}))
        };
        let mut description = t["description"].as_str().unwrap_or("").to_owned();
        if custom {
            if let Some(format) = t.get("format") {
                description.push_str(&format!(
                    "\nRaw tool input must follow this format: {format}"
                ));
            }
        }
        schemas.push(json!({"name":upstream,"description":description,"input_schema":parameters}));
        mapping.push(Tool {
            upstream,
            name,
            namespace: namespace.map(str::to_owned),
            custom,
        });
        Ok(())
    }
    let mut schemas = Vec::new();
    let mut mapping = Vec::new();
    if let Some(list) = request.get("tools") {
        for t in list.as_array().ok_or("tools 必须是数组")? {
            add(t, None, &mut schemas, &mut mapping)?;
        }
    }
    Ok((schemas, mapping))
}

pub fn request(request: &Value, stream: bool) -> ConversionResult<Converted> {
    let model = crate::types::ProviderModel {
        id: request["model"].as_str().unwrap_or("").into(),
        ..Default::default()
    };
    request_with_model(request, stream, &model)
}

pub fn request_with_model(
    request: &Value,
    stream: bool,
    configured: &crate::types::ProviderModel,
) -> ConversionResult<Converted> {
    let profile = crate::model_capabilities::profile(configured);
    if profile.endpoints.messages == Some(false) {
        return Err("当前模型未提供 Claude Messages 接口，请检查模型接口声明".into());
    }
    if request
        .get("previous_response_id")
        .is_some_and(|v| !v.is_null())
    {
        return Err("本地转换使用完整对话历史，不支持 previous_response_id".into());
    }
    let model = request["model"].as_str().ok_or("请求缺少 model")?;
    let (schemas, mapping) = tools(request)?;
    let mut messages = Vec::new();
    let mut system = Vec::new();
    if let Some(instructions) = request.get("instructions").filter(|v| !v.is_null()) {
        system.extend(text(instructions)?);
    }
    let input = request.get("input").ok_or("请求缺少 input")?;
    let items = if input.is_string() {
        vec![json!({"role":"user","content":input})]
    } else {
        input.as_array().ok_or("input 必须是文本或数组")?.clone()
    };
    for item in items {
        match item["type"].as_str().unwrap_or("message") {
            "message" => {
                let role = item["role"].as_str().ok_or("消息缺少 role")?;
                let content = text(&item["content"])?;
                match role {
                    "system" | "developer" => system.extend(content),
                    "user" | "assistant" => push(&mut messages, role, content),
                    _ => return Err(format!("不支持的消息角色：{role}")),
                }
            }
            "function_call" | "custom_tool_call" => {
                let name = item["name"].as_str().ok_or("工具调用缺少 name")?;
                let spec = mapping.iter().find(|t| {
                    t.name == name && t.namespace.as_deref() == item["namespace"].as_str()
                });
                let args = if item["type"] == "custom_tool_call" {
                    json!({"input":item["input"]})
                } else {
                    serde_json::from_str(
                        item["arguments"].as_str().ok_or("工具调用缺少 arguments")?,
                    )
                    .map_err(|_| "工具调用 arguments 不是有效 JSON")?
                };
                push(
                    &mut messages,
                    "assistant",
                    vec![
                        json!({"type":"tool_use","id":item["call_id"],"name":spec.map(|t| t.upstream.as_str()).unwrap_or(name),"input":args}),
                    ],
                );
            }
            "function_call_output" | "custom_tool_call_output" => {
                let output = item.get("output").ok_or("工具结果缺少 output")?;
                let content = if output.is_string() || output.is_array() {
                    text(output)?
                } else {
                    vec![json!({"type":"text","text":output.to_string()})]
                };
                push(
                    &mut messages,
                    "user",
                    vec![
                        json!({"type":"tool_result","tool_use_id":item["call_id"],"content":content}),
                    ],
                );
            }
            "reasoning" => {
                if let Some(block) = item["encrypted_content"]
                    .as_str()
                    .and_then(|s| decode(s, THINKING_PREFIX))
                    .and_then(|s| serde_json::from_str::<Value>(&s).ok())
                {
                    if matches!(
                        block["type"].as_str(),
                        Some("thinking" | "redacted_thinking")
                    ) {
                        push(&mut messages, "assistant", vec![block]);
                    }
                }
            }
            "compaction" => {
                let summary = item["encrypted_content"]
                    .as_str()
                    .and_then(|s| decode(s, COMPACT_PREFIX))
                    .ok_or("此压缩记录不是 Claude 转换服务生成的，请新建对话")?;
                push(
                    &mut messages,
                    "user",
                    vec![
                        json!({"type":"text","text":format!("Previous conversation summary:\n{summary}")}),
                    ],
                );
            }
            other => return Err(format!("协议转换暂不支持 input 类型：{other}")),
        }
    }
    if messages.is_empty() {
        return Err("请求没有可发送的对话内容".into());
    }
    let max_tokens = request["max_output_tokens"]
        .as_u64()
        .unwrap_or(8192)
        .clamp(1, profile.max_output_tokens.unwrap_or(128_000) as u64);
    let mut body =
        json!({"model":model,"messages":messages,"max_tokens":max_tokens,"stream":stream});
    if !system.is_empty() {
        body["system"] = json!(system);
    }
    if !schemas.is_empty() && profile.tool_calls == Some(false) {
        return Err("当前接入路径明确不支持工具调用".into());
    }
    if !schemas.is_empty() {
        body["tools"] = json!(schemas);
    }
    if let Some(choice) = request.get("tool_choice") {
        let value = match choice.as_str() {
            Some("auto") => json!({"type":"auto"}),
            Some("required") => json!({"type":"any"}),
            Some("none") => json!({"type":"none"}),
            _ => {
                let name = choice["name"].as_str().ok_or("不支持此 tool_choice")?;
                let spec = mapping
                    .iter()
                    .find(|t| {
                        t.name == name && t.namespace.as_deref() == choice["namespace"].as_str()
                    })
                    .ok_or("tool_choice 指定了不存在的工具")?;
                json!({"type":"tool","name":spec.upstream})
            }
        };
        body["tool_choice"] = value;
    }
    if request["parallel_tool_calls"] == false && !schemas.is_empty() {
        if body.get("tool_choice").is_none() {
            body["tool_choice"] = json!({"type":"auto"});
        }
        body["tool_choice"]["disable_parallel_tool_use"] = json!(true);
    }
    if profile.thinking_format == Some(crate::types::ThinkingFormat::Deepseek)
        && request
            .pointer("/reasoning/effort")
            .is_some_and(|effort| effort == "none")
    {
        body["thinking"] = json!({"type":"disabled"});
    }
    if let Some(effort) = request
        .pointer("/reasoning/effort")
        .and_then(Value::as_str)
        .filter(|e| *e != "none")
    {
        // Forced tool selection and extended thinking cannot be combined in Messages.
        let forced = matches!(body["tool_choice"]["type"].as_str(), Some("tool" | "any"));
        if !forced
            && (max_tokens > 1024
                || matches!(
                    profile.thinking_format,
                    Some(
                        crate::types::ThinkingFormat::Adaptive
                            | crate::types::ThinkingFormat::Deepseek
                    )
                ))
        {
            let format = profile.thinking_format;
            if format == Some(crate::types::ThinkingFormat::Adaptive) {
                body["thinking"] = json!({"type":"adaptive"});
                let effort = crate::model_capabilities::mapped_effort(&profile, effort)?;
                body["output_config"] = json!({"effort":effort});
            } else if format == Some(crate::types::ThinkingFormat::Deepseek) {
                body["thinking"] = json!({"type":"enabled"});
                body["output_config"] =
                    json!({"effort":crate::model_capabilities::mapped_effort(&profile, effort)?});
            } else if format == Some(crate::types::ThinkingFormat::Budget) {
                let budget = match effort {
                    "minimal" | "low" => 1024,
                    "medium" => 2048,
                    "high" => 4096,
                    _ => 6144,
                };
                body["thinking"] =
                    json!({"type":"enabled","budget_tokens":budget.min(max_tokens - 1)});
            } else if format != Some(crate::types::ThinkingFormat::None) {
                return Err(
                    "未确认此模型的 Claude 思考参数，请同步元数据或在模型配置中指定思考模式".into(),
                );
            }
        }
    }
    if body.get("thinking").is_none() && profile.sampling_parameters != Some(false) {
        if let Some(value) = request.get("temperature") {
            body["temperature"] = value.clone();
        }
        if let Some(value) = request.get("top_p") {
            body["top_p"] = value.clone();
        }
    }
    if request["service_tier"] == "priority" {
        return Err("Claude Messages 转换暂不支持 Codex 的 priority/Fast 档位，请关闭 Fast 模式或使用客户端默认值".into());
    }
    Ok(Converted {
        body,
        tools: mapping,
    })
}

pub fn usage(value: &Value) -> Value {
    let input = value["input_tokens"].as_u64().unwrap_or(0);
    let cached = value["cache_read_input_tokens"].as_u64().unwrap_or(0);
    let written = value["cache_creation_input_tokens"].as_u64().unwrap_or(0);
    let output = value["output_tokens"].as_u64().unwrap_or(0);
    json!({"input_tokens":input+cached+written,"input_tokens_details":{"cached_tokens":cached},"output_tokens":output,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":input+cached+written+output})
}
pub fn response(model: &str, output: Vec<Value>, tokens: &Value, stop: &str) -> Value {
    let incomplete = stop == "max_tokens";
    json!({"id":format!("resp_{}",uuid::Uuid::new_v4().simple()),"object":"response","created_at":std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).unwrap_or_default().as_secs(),"model":model,"status":if incomplete {"incomplete"} else {"completed"},"error":null,"incomplete_details":if incomplete {json!({"reason":"max_output_tokens"})} else {Value::Null},"output":output,"usage":usage(tokens)})
}
pub fn item(block: &Value, tools: &[Tool], id: &str) -> ConversionResult<Value> {
    match block["type"].as_str().unwrap_or("") {
        "text" => Ok(
            json!({"id":id,"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":block["text"],"annotations":[]}]}),
        ),
        "thinking" | "redacted_thinking" => Ok(
            json!({"id":id,"type":"reasoning","summary":block["thinking"].as_str().filter(|s| !s.is_empty()).map(|s|vec![json!({"type":"summary_text","text":s})]).unwrap_or_default(),"encrypted_content":opaque_thinking(block)}),
        ),
        "tool_use" => {
            let name = block["name"].as_str().ok_or("Claude 工具调用缺少 name")?;
            let spec = tools
                .iter()
                .find(|t| t.upstream == name)
                .ok_or_else(|| format!("Claude 返回了未声明的工具：{name}"))?;
            let mut value = if spec.custom {
                json!({"id":id,"type":"custom_tool_call","call_id":block["id"],"name":spec.name,"input":block["input"]["input"].as_str().ok_or("Claude 自定义工具缺少字符串 input")?,"status":"completed"})
            } else {
                json!({"id":id,"type":"function_call","call_id":block["id"],"name":spec.name,"arguments":block["input"].to_string(),"status":"completed"})
            };
            if let Some(namespace) = &spec.namespace {
                value["namespace"] = json!(namespace);
            }
            Ok(value)
        }
        other => Err(format!("Claude 返回了不支持的内容类型：{other}")),
    }
}
