import assert from "node:assert/strict";
import path from "node:path";
import { createRequire } from "node:module";
import { pathToFileURL } from "node:url";
import { readFile, writeFile } from "node:fs/promises";

// Dependencies live in an isolated QA directory, never in the application bundle.
const sdkRoot = path.resolve(".qa/extra-client-audit/sdk");
const sdkRequire = createRequire(path.join(sdkRoot, "package.json"));
const load = (name) => {
  // pi-ai exposes these modules with import-only export conditions.
  if (name.startsWith("@earendil-works/pi-ai/")) {
    const subpath = name.slice("@earendil-works/pi-ai/".length);
    return import(
      pathToFileURL(
        path.join(
          sdkRoot,
          "node_modules/@earendil-works/pi-ai/dist",
          subpath + ".js",
        ),
      ).href
    );
  }
  return import(pathToFileURL(sdkRequire.resolve(name)).href);
};
const prefillError =
  "This model does not support assistant message prefill. The conversation must end with a user message.";
const systemRoleError = "Claude 消息 role 必须为 user 或 assistant";
const key = "synthetic-native-protocol-key";
const modelId = "qa-native-model";
const params = {
  type: "object",
  properties: { path: { type: "string" } },
  required: ["path"],
};
const cost = { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 };
const usage = {
  input: 1,
  output: 1,
  cacheRead: 0,
  cacheWrite: 0,
  totalTokens: 2,
  cost: { ...cost, total: 0 },
};
const textPart = (text) => ({ type: "text", text });
const aiTools = [
  {
    type: "function",
    name: "read_file",
    description: "QA file reader",
    inputSchema: params,
  },
];

function reply(protocol, stream) {
  if (protocol === "messages") {
    const message = {
      id: "msg_qa",
      type: "message",
      role: "assistant",
      model: modelId,
      content: [textPart("qa-ok")],
      stop_reason: "end_turn",
      stop_sequence: null,
      usage: { input_tokens: 1, output_tokens: 1 },
    };
    if (!stream) return Response.json(message);
    return sse([
      {
        type: "message_start",
        message: {
          ...message,
          content: [],
          stop_reason: null,
          usage: { input_tokens: 1, output_tokens: 0 },
        },
      },
      { type: "content_block_start", index: 0, content_block: textPart("") },
      {
        type: "content_block_delta",
        index: 0,
        delta: { type: "text_delta", text: "qa-ok" },
      },
      { type: "content_block_stop", index: 0 },
      {
        type: "message_delta",
        delta: { stop_reason: "end_turn", stop_sequence: null },
        usage: { output_tokens: 1 },
      },
      { type: "message_stop" },
    ]);
  }
  if (protocol === "chat_completions") {
    const base = { id: "chat_qa", model: modelId, created: 1 };
    if (!stream)
      return Response.json({
        ...base,
        object: "chat.completion",
        choices: [
          {
            index: 0,
            message: { role: "assistant", content: "qa-ok" },
            finish_reason: "stop",
          },
        ],
        usage: { prompt_tokens: 1, completion_tokens: 1, total_tokens: 2 },
      });
    return sse([
      {
        ...base,
        object: "chat.completion.chunk",
        choices: [
          {
            index: 0,
            delta: { role: "assistant", content: "qa-ok" },
            finish_reason: null,
          },
        ],
      },
      {
        ...base,
        object: "chat.completion.chunk",
        choices: [{ index: 0, delta: {}, finish_reason: "stop" }],
        usage: { prompt_tokens: 1, completion_tokens: 1, total_tokens: 2 },
      },
      "[DONE]",
    ]);
  }
  const part = {
    type: "output_text",
    text: "qa-ok",
    annotations: [],
    logprobs: [],
  };
  const item = {
    id: "msg_qa",
    type: "message",
    role: "assistant",
    status: "completed",
    content: [part],
  };
  const response = {
    id: "resp_qa",
    object: "response",
    model: modelId,
    created_at: 1,
    status: "completed",
    output: [item],
    usage: {
      input_tokens: 1,
      output_tokens: 1,
      total_tokens: 2,
      input_tokens_details: { cached_tokens: 0 },
      output_tokens_details: { reasoning_tokens: 0 },
    },
  };
  if (!stream) return Response.json(response);
  return sse([
    {
      type: "response.created",
      response: { ...response, status: "in_progress", output: [] },
    },
    {
      type: "response.output_item.added",
      output_index: 0,
      item: { ...item, status: "in_progress", content: [] },
    },
    {
      type: "response.content_part.added",
      item_id: item.id,
      output_index: 0,
      content_index: 0,
      part: { ...part, text: "" },
    },
    {
      type: "response.output_text.delta",
      item_id: item.id,
      output_index: 0,
      content_index: 0,
      delta: "qa-ok",
      logprobs: [],
    },
    {
      type: "response.output_text.done",
      item_id: item.id,
      output_index: 0,
      content_index: 0,
      text: "qa-ok",
      logprobs: [],
    },
    {
      type: "response.content_part.done",
      item_id: item.id,
      output_index: 0,
      content_index: 0,
      part,
    },
    { type: "response.output_item.done", output_index: 0, item },
    { type: "response.completed", response },
  ]);
}
function sse(events) {
  return new Response(
    events
      .map((event, i) =>
        typeof event === "string"
          ? "data: " + event + "\n\n"
          : (event.type ? "event: " + event.type + "\n" : "") +
            "data: " +
            JSON.stringify({ ...event, sequence_number: i }) +
            "\n\n",
      )
      .join(""),
    { headers: { "content-type": "text/event-stream" } },
  );
}
function assertPayload(body, protocol, scenario) {
  assert.equal(body.model, modelId);
  const wire = JSON.stringify(body);
  assert.ok(wire.includes("history-question"));
  if (scenario === "history" || scenario === "system") {
    assert.ok(wire.includes("history-answer"));
    assert.ok(wire.includes("new-question"));
  }
  if (scenario === "tools") {
    assert.ok(wire.includes("call_qa"));
    assert.ok(wire.includes("tool-output-marker"));
    assert.ok(wire.includes("read_file"));
  }
  if (scenario === "system") assert.ok(wire.includes("mid-system-marker"));
  if (protocol === "responses") {
    assert.ok(Array.isArray(body.input));
    assert.equal(body.messages, undefined);
    if (scenario === "tools") {
      const call = body.input.find((i) => i.type === "function_call");
      const result = body.input.find((i) => i.type === "function_call_output");
      assert.ok(call && result);
      assert.equal(result.call_id, call.call_id);
      assert.ok(body.input.indexOf(call) < body.input.indexOf(result));
    }
  } else {
    assert.ok(Array.isArray(body.messages));
    assert.equal(body.input, undefined);
    if (protocol === "messages") {
      assert.ok(
        body.messages.every((m) =>
          [
            "user",
            "assistant",
            ...(scenario === "system" ? ["system"] : []),
          ].includes(m.role),
        ),
      );
      assert.equal(
        body.messages.at(-1).role,
        scenario === "prefill" ? "assistant" : "user",
      );
      if (scenario === "tools") {
        const blocks = body.messages.flatMap((m) =>
          Array.isArray(m.content) ? m.content : [],
        );
        const call = blocks.find((i) => i.type === "tool_use");
        const result = blocks.find((i) => i.type === "tool_result");
        assert.equal(result.tool_use_id, call.id);
        assert.ok(blocks.indexOf(call) < blocks.indexOf(result));
      }
    } else if (scenario === "tools") {
      const call = body.messages.find((m) => m.tool_calls?.length)
        ?.tool_calls[0];
      const result = body.messages.find((m) => m.role === "tool");
      assert.equal(result.tool_call_id, call.id);
    }
  }
}
function aiPrompt(scenario) {
  const initial = { role: "user", content: [textPart("history-question")] };
  if (scenario === "tools")
    return [
      initial,
      {
        role: "assistant",
        content: [
          {
            type: "tool-call",
            toolCallId: "call_qa",
            toolName: "read_file",
            input: { path: "qa.txt" },
          },
        ],
      },
      {
        role: "tool",
        content: [
          {
            type: "tool-result",
            toolCallId: "call_qa",
            toolName: "read_file",
            output: { type: "text", value: "tool-output-marker" },
          },
        ],
      },
    ];
  const prompt = [
    initial,
    { role: "assistant", content: [textPart("history-answer")] },
  ];
  if (scenario === "system") {
    prompt.unshift({ role: "system", content: "leading-system-marker" });
    prompt.push({ role: "system", content: "mid-system-marker" });
  }
  if (scenario !== "prefill")
    prompt.push({ role: "user", content: [textPart("new-question")] });
  return prompt;
}
function piContext(scenario, api) {
  const initial = { role: "user", content: "history-question", timestamp: 1 };
  const assistant = {
    role: "assistant",
    content: [textPart("history-answer")],
    api,
    provider: "uni-switch",
    model: modelId,
    usage,
    stopReason: "stop",
    timestamp: 2,
  };
  if (scenario === "tools")
    return {
      messages: [
        initial,
        {
          ...assistant,
          content: [
            {
              type: "toolCall",
              id: "call_qa",
              name: "read_file",
              arguments: { path: "qa.txt" },
            },
          ],
          stopReason: "toolUse",
        },
        {
          role: "toolResult",
          toolCallId: "call_qa",
          toolName: "read_file",
          content: [textPart("tool-output-marker")],
          isError: false,
          timestamp: 3,
        },
      ],
      tools: [
        {
          name: "read_file",
          description: "QA file reader",
          parameters: params,
        },
      ],
    };
  const messages = [initial, assistant];
  if (scenario === "system") {
    messages.unshift({
      role: "system",
      content: "leading-system-marker",
      timestamp: 0,
    });
    messages.push({
      role: "system",
      content: "mid-system-marker",
      timestamp: 3,
    });
  }
  if (scenario !== "prefill")
    messages.push({ role: "user", content: "new-question", timestamp: 3 });
  return { messages };
}
export async function runNativeClientProtocols(fixtures) {
  const [
    { createAnthropic },
    { createOpenAI },
    { createOpenAICompatible },
    { parse },
    { normalizeContext },
  ] = await Promise.all([
    load("@ai-sdk/anthropic"),
    load("@ai-sdk/openai"),
    load("@ai-sdk/openai-compatible"),
    load("yaml"),
    load("@earendil-works/pi-ai/utils/transcript"),
  ]);
  const pi = {
    messages: await load("@earendil-works/pi-ai/api/anthropic-messages"),
    chat_completions: await load(
      "@earendil-works/pi-ai/api/openai-completions",
    ),
    responses: await load("@earendil-works/pi-ai/api/openai-responses"),
  };
  const report = {
    configurationCases: fixtures.length,
    sdkRequests: 0,
    sdkSuccesses: 0,
    workbuddySchemaCases: 0,
    prefillReproductions: [],
    legacySystemRoleReproductions: [],
    versions: {},
    boundaries: [
      "ZCode uses its upstream AI SDK versions; ordinary text/tool paths only, without ZCode video patch or full agent loop",
      "DSH uses pi-ai 1.0.2 serializers; no complete DSH UI/agent loop",
      "WorkBuddy validates documented models.json only; its executable/serializer is not installed",
    ],
  };
  for (const name of [
    "@ai-sdk/anthropic",
    "@ai-sdk/openai",
    "@ai-sdk/openai-compatible",
    "@earendil-works/pi-ai",
  ]) {
    const dir = path.join(sdkRoot, "node_modules", ...name.split("/"));
    report.versions[name] = JSON.parse(
      await readFile(path.join(dir, "package.json"), "utf8"),
    ).version;
  }
  assert.deepEqual(report.versions, {
    "@ai-sdk/anthropic": "3.0.81",
    "@ai-sdk/openai": "3.0.58",
    "@ai-sdk/openai-compatible": "2.0.60",
    "@earendil-works/pi-ai": "1.0.2",
  });
  for (const fixture of fixtures) {
    const { client, protocol, auth, expectedEndpoint } = fixture;
    if (client === "workbuddy") {
      const entry = JSON.parse(fixture.contents[0]).models.find(
        (m) => m.id === modelId,
      );
      assert.equal(entry.url, expectedEndpoint);
      assert.equal(entry.vendor, "OpenAI");
      assert.equal(entry.apiKey, key);
      assert.equal(entry.api, undefined);
      report.workbuddySchemaCases++;
      continue;
    }
    let config, apiKey;
    if (client === "zcode") {
      config = JSON.parse(
        fixture.contents[0],
      ).config.providerConfigRules.providerRules.find(
        (r) => r.providerId === "uni-switch",
      ).config;
      apiKey = config.access.apiKey;
      assert.equal(
        config.api.type,
        {
          messages: "anthropic-messages",
          chat_completions: "openai-chat-completions",
          responses: "openai-responses",
        }[protocol],
      );
    } else {
      const managedBlock = fixture.contents[0]
        .split("# uni-switch managed begin\n")[1]
        ?.split("# uni-switch managed end")[0];
      assert.ok(managedBlock, "DSH config must contain its managed block");
      config = parse(managedBlock)
        .find((p) => p.insert?.some((i) => i.id === "uni-switch-llm"))
        .insert.find((i) => i.id === "uni-switch-llm").config.providers[
        "uni-switch"
      ];
      apiKey = parse(fixture.contents[1]).refs[config.apiKeyEnv];
      assert.equal(
        config.api,
        {
          messages: "anthropic-messages",
          chat_completions: "openai-completions",
          responses: "openai-responses",
        }[protocol],
      );
    }
    assert.equal(apiKey, key);
    const scenarios = ["history", "tools"];
    if (fixture.pathCase === "origin") scenarios.push("system");
    if (protocol === "messages" && fixture.pathCase === "origin")
      scenarios.push("prefill");
    for (const scenario of scenarios) {
      for (const stream of client === "zcode" && scenario !== "prefill"
        ? [false, true]
        : [true]) {
        let requests = 0;
        let wireFailure;
        const capture = async (input, init) => {
          requests++;
          try {
            const request = new Request(input, init);
            const requestUrl = new URL(request.url);
            if (client === "dsh" && protocol === "messages") {
              assert.equal(requestUrl.search, "?beta=true");
              requestUrl.search = "";
            }
            assert.equal(
              requestUrl.href,
              expectedEndpoint,
              client + " " + protocol + " " + fixture.pathCase,
            );
            assert.equal(request.method, "POST");
            assert.equal(
              request.headers.get(
                auth === "x-api-key" ? "x-api-key" : "authorization",
              ),
              auth === "x-api-key" ? key : "Bearer " + key,
            );
            if (protocol === "messages")
              assert.ok(request.headers.has("anthropic-version"));
            const body = JSON.parse(await request.text());
            assertPayload(body, protocol, scenario);
            if (scenario === "system" && protocol === "messages") {
              const inHistorySystem = body.messages.some(
                (m) => m.role === "system",
              );
              assert.equal(inHistorySystem, client === "zcode");
              if (inHistorySystem) {
                assert.ok(
                  request.headers
                    .get("anthropic-beta")
                    ?.includes("mid-conversation-system-2026-04-07"),
                );
                return Response.json(
                  {
                    type: "error",
                    error: {
                      type: "invalid_request_error",
                      message: systemRoleError,
                    },
                  },
                  { status: 400 },
                );
              }
            }
            if (scenario === "prefill")
              return Response.json(
                {
                  type: "error",
                  error: {
                    type: "invalid_request_error",
                    message: prefillError,
                  },
                },
                { status: 400 },
              );
            return reply(protocol, stream);
          } catch (e) {
            wireFailure = e;
            throw e;
          }
        };
        let error,
          resultText = "",
          finished = false;
        try {
          if (client === "zcode") {
            let baseURL = config.api.baseUrl;
            const headers = { ...config.api.headers };
            if (protocol === "messages") {
              if (!baseURL.endsWith("/v1")) baseURL += "/v1";
              if (
                !Object.keys(headers).some(
                  (h) => h.toLowerCase() === "authorization",
                )
              )
                headers.Authorization = "Bearer " + apiKey;
            }
            const options = { apiKey, baseURL, headers, fetch: capture };
            const model =
              protocol === "messages"
                ? createAnthropic(options)(modelId)
                : protocol === "responses"
                  ? createOpenAI(options).responses(modelId)
                  : createOpenAICompatible({
                      ...options,
                      name: "uni-switch",
                      includeUsage: true,
                      supportsStructuredOutputs: false,
                    })(modelId);
            const request = {
              prompt: aiPrompt(scenario),
              tools: aiTools,
              maxOutputTokens: 1024,
            };
            if (stream) {
              const result = await model.doStream(request);
              for await (const event of result.stream) {
                if (event.type === "error") throw event.error;
                if (event.type === "text-delta") resultText += event.delta;
                if (event.type === "finish") finished = true;
              }
            } else {
              const result = await model.doGenerate(request);
              resultText = result.content
                .filter((c) => c.type === "text")
                .map((c) => c.text)
                .join("");
              finished = true;
            }
          } else {
            const model = {
              id: modelId,
              name: modelId,
              api: config.api,
              provider: "uni-switch",
              baseUrl: config.baseURL,
              headers: config.headers,
              input: ["text"],
              reasoning: false,
              cost,
              contextWindow: 128000,
              maxTokens: 8192,
            };
            const result = pi[protocol].streamSimple(
              model,
              normalizeContext(piContext(scenario, config.api)),
              { apiKey, fetch: capture, maxTokens: 1024, maxRetries: 0 },
            );
            for await (const event of result) {
              if (event.type === "error")
                throw new Error(event.error.errorMessage);
              if (event.type === "text_delta") resultText += event.delta;
              if (event.type === "done") finished = true;
            }
          }
        } catch (e) {
          error = e;
        }
        if (wireFailure) throw wireFailure;
        assert.ok(requests > 0, "SDK did not send a request: " + error);
        report.sdkRequests += requests;
        if (scenario === "prefill") {
          assert.ok(
            error?.message.includes(prefillError),
            "expected upstream prefill limitation: " + error,
          );
          report.prefillReproductions.push({
            client,
            auth,
            sdkRequestEndsWithAssistant: true,
            upstreamStatus: 400,
          });
        } else if (
          scenario === "system" &&
          client === "zcode" &&
          protocol === "messages"
        ) {
          assert.ok(
            error?.message.includes(systemRoleError),
            "expected legacy gateway system-role limitation: " + error,
          );
          report.legacySystemRoleReproductions.push({
            client,
            auth,
            stream,
            upstreamStatus: 400,
          });
        } else {
          if (error)
            throw new Error(
              client +
                " " +
                protocol +
                " " +
                fixture.pathCase +
                " " +
                auth +
                " " +
                scenario +
                ": " +
                error.message,
              { cause: error },
            );
          assert.equal(resultText, "qa-ok");
          assert.equal(finished, true);
          report.sdkSuccesses++;
        }
      }
    }
  }
  return report;
}
if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href
) {
  assert.ok(
    process.argv[2],
    "Pass protocol-fixtures.json generated by qa-client-config.mjs --protocols",
  );
  const fixtures = JSON.parse(await readFile(process.argv[2], "utf8"));
  const report = await runNativeClientProtocols(fixtures);
  await writeFile(
    path.join(path.dirname(process.argv[2]), "native-protocol-report.json"),
    JSON.stringify(report, null, 2),
    "utf8",
  );
  console.log(JSON.stringify(report, null, 2));
}
