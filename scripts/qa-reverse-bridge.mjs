import { chromium } from "playwright";
import AxeBuilder from "@axe-core/playwright";
import http from "node:http";
import { spawn, execFile } from "node:child_process";
import { promisify } from "node:util";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import assert from "node:assert/strict";

const root = path.resolve(".qa/reverse-bridge/native", String(Date.now()));
const cliDir = path.join(root, "claude-cli"),
  work = path.join(root, "work");
await mkdir(work, { recursive: true });
await mkdir(cliDir, { recursive: true });
await writeFile(
  path.join(work, "input.txt"),
  "GPT bridge real Claude tool read verified.\n",
);
await writeFile(
  path.join(cliDir, "settings.json"),
  JSON.stringify({
    env: { ENABLE_TOOL_SEARCH: "auto:10", CUSTOM_QA: "preserve" },
  }),
);
const key = "fake-openai-key-only-for-qa",
  ids = ["gpt-5.4", "gpt-4.1"];
const requests = [],
  checks = [];
let mode = "text",
  toolResult = "",
  disconnected = false;
function emit(res, event) {
  res.write(`data: ${JSON.stringify(event)}\n\n`);
}
const server = http.createServer(async (req, res) => {
  if (req.headers.authorization !== `Bearer ${key}`) {
    res.writeHead(401, { "Content-Type": "application/json" });
    res.end(
      JSON.stringify({ error: { message: "Missing QA authentication" } }),
    );
    return;
  }
  if (req.url === "/v1/models") {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ data: ids.map((id) => ({ id })) }));
    return;
  }
  if (!["/v1/responses", "/v1/chat/completions"].includes(req.url)) {
    res.writeHead(404);
    res.end("{}");
    return;
  }
  let raw = "";
  for await (const c of req) raw += c;
  const body = JSON.parse(raw),
    chat = req.url.endsWith("completions");
  requests.push({ url: req.url, body });
  if (mode.startsWith("chat") && !chat) {
    res.writeHead(404, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ error: { message: "Responses not supported" } }));
    return;
  }
  if (mode === "http-error") {
    res.writeHead(429, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ error: { message: `rate limit ${key}` } }));
    return;
  }
  if (mode === "cancel") {
    res.writeHead(200, { "Content-Type": "text/event-stream" });
    emit(res, {
      type: "response.created",
      response: { status: "in_progress" },
    });
    res.on("close", () => {
      disconnected = true;
    });
    return;
  }
  if (mode === "malformed") {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end("{}");
    return;
  }
  const history = chat ? body.messages : body.input;
  const results = history.filter((i) =>
    chat ? i.role === "tool" : i.type === "function_call_output",
  );
  if (results.length) toolResult = JSON.stringify(results);
  const toolMode =
    ["read", "write", "chat-read"].includes(mode) && !results.length;
  let output;
  if (toolMode) {
    const name = mode === "write" ? "Write" : "Read";
    const tool = body.tools?.find((t) =>
      chat ? t.function.name === name : t.name === name,
    );
    assert.ok(tool, `Missing ${name} tool`);
    const args =
      mode === "write"
        ? {
            file_path: path.join(work, "created.txt"),
            content: "GPT bridge real Claude write verified.\n",
          }
        : { file_path: path.join(work, "input.txt") };
    output = [
      {
        type: "reasoning",
        id: "rs_qa",
        summary: [{ type: "summary_text", text: "Read the isolated fixture." }],
        encrypted_content: "encrypted_qa",
      },
      {
        type: "function_call",
        id: "fc_qa",
        call_id: "call_qa",
        name,
        arguments: JSON.stringify(args, null, 1),
      },
    ];
  } else
    output = [
      {
        type: "message",
        id: "msg_qa",
        role: "assistant",
        status: "completed",
        content: [
          {
            type: "output_text",
            text: "GPT converted reply · 中文正常",
            annotations: [],
          },
        ],
      },
    ];
  const usage = {
    input_tokens: 20,
    output_tokens: 5,
    input_tokens_details: { cached_tokens: 3 },
  };
  if (chat) {
    const tool = output.find((o) => o.type === "function_call"),
      text = tool ? null : output[0].content[0].text;
    if (!body.stream) {
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(
        JSON.stringify({
          id: "chat_qa",
          choices: [
            {
              message: {
                role: "assistant",
                content: text,
                ...(tool
                  ? {
                      tool_calls: [
                        {
                          id: tool.call_id,
                          type: "function",
                          function: {
                            name: tool.name,
                            arguments: tool.arguments,
                          },
                        },
                      ],
                    }
                  : {}),
              },
              finish_reason: tool ? "tool_calls" : "stop",
            },
          ],
          usage: { prompt_tokens: 20, completion_tokens: 5 },
        }),
      );
      return;
    }
    res.writeHead(200, { "Content-Type": "text/event-stream" });
    if (tool) {
      emit(res, {
        choices: [
          {
            delta: {
              tool_calls: [
                {
                  index: 0,
                  id: tool.call_id,
                  type: "function",
                  function: {
                    name: tool.name,
                    arguments: tool.arguments.slice(0, 8),
                  },
                },
              ],
            },
            finish_reason: null,
          },
        ],
      });
      emit(res, {
        choices: [
          {
            delta: {
              tool_calls: [
                { index: 0, function: { arguments: tool.arguments.slice(8) } },
              ],
            },
            finish_reason: "tool_calls",
          },
        ],
      });
    } else {
      emit(res, {
        choices: [
          { delta: { content: text.slice(0, 8) }, finish_reason: null },
        ],
      });
      emit(res, {
        choices: [{ delta: { content: text.slice(8) }, finish_reason: "stop" }],
      });
    }
    emit(res, {
      choices: [],
      usage: {
        prompt_tokens: 20,
        completion_tokens: 5,
        prompt_tokens_details: { cached_tokens: 3 },
      },
    });
    res.end("data: [DONE]\n\n");
    return;
  }
  const response = {
    id: "resp_qa",
    object: "response",
    status: "completed",
    model: body.model,
    output,
    usage,
  };
  if (!body.stream) {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(JSON.stringify(response));
    return;
  }
  res.writeHead(200, { "Content-Type": "text/event-stream" });
  emit(res, {
    type: "response.created",
    response: { ...response, status: "in_progress", output: [] },
  });
  if (mode === "stream-error") {
    emit(res, { type: "error", message: `overloaded ${key}` });
    res.end();
    return;
  }
  for (const [index, item] of output.entries()) {
    emit(res, {
      type: "response.output_item.added",
      output_index: index,
      item: {
        ...item,
        ...(item.type === "function_call" ? { arguments: "" } : {}),
      },
    });
    if (item.type === "function_call") {
      for (let i = 0; i < item.arguments.length; i += 7)
        emit(res, {
          type: "response.function_call_arguments.delta",
          output_index: index,
          delta: item.arguments.slice(i, i + 7),
        });
    } else if (item.type === "reasoning")
      emit(res, {
        type: "response.reasoning_summary_text.delta",
        output_index: index,
        summary_index: 0,
        delta: item.summary[0].text,
      });
    else {
      const bytes = Buffer.from(
        `data: ${JSON.stringify({ type: "response.output_text.delta", output_index: index, content_index: 0, delta: item.content[0].text })}\n\n`,
      );
      for (let i = 0; i < bytes.length; i += 2)
        res.write(bytes.subarray(i, i + 2));
    }
    emit(res, { type: "response.output_item.done", output_index: index, item });
  }
  if (mode !== "truncated") emit(res, { type: "response.completed", response });
  res.end();
});
await new Promise((r) => server.listen(0, "127.0.0.1", r));
const origin = `http://127.0.0.1:${server.address().port}`;
async function until(fn, timeout = 30000) {
  const end = Date.now() + timeout;
  while (Date.now() < end) {
    const value = await fn();
    if (value) return value;
    await new Promise((r) => setTimeout(r, 100));
  }
  throw Error("QA timeout");
}
let child, browser, page;
const appEnv = {
  ...process.env,
  UNI_SWITCH_DATA_DIR: path.join(root, "data"),
  WEBVIEW2_USER_DATA_FOLDER: path.join(root, "webview"),
  CODEX_HOME: path.join(root, "codex"),
  CLAUDE_CONFIG_DIR: cliDir,
  LOCALAPPDATA: path.join(root, "local"),
};
const exe = path.resolve(
  process.env.UNI_SWITCH_QA_EXE || ".qa/reverse-bridge/test-app/uni-switch.exe",
);
try {
  try {
    assert.equal((await fetch("http://127.0.0.1:9223/json/version")).ok, false);
  } catch (e) {
    if (e.code === "ERR_ASSERTION") throw e;
  }
  child = spawn(exe, [], { windowsHide: true, stdio: "ignore", env: appEnv });
  await until(async () => {
    try {
      return (await fetch("http://127.0.0.1:9223/json/version")).ok;
    } catch {
      return false;
    }
  });
  browser = await chromium.connectOverCDP("http://127.0.0.1:9223");
  page = browser.contexts()[0].pages()[0];
  page.setDefaultTimeout(20000);
  const invoke = (name, args = {}) =>
    page.evaluate(
      ({ name, args }) => window.__TAURI_INTERNALS__.invoke(name, args),
      { name, args },
    );
  await page.getByRole("tab", { name: /Claude/ }).click();
  await invoke("set_directory", {target:"claude_cli", directory:cliDir});
  await page.getByRole("button", { name: /^添加(第一个)?供应商$/ }).click();
  await page.locator(".form-advanced > summary").click();
  assert.equal(await page.getByLabel("API 协议").inputValue(), "auto");
  await page.getByLabel("API 协议").selectOption("openai");
  await page.locator("#baseUrl").fill(origin);
  await page.locator("#apiKey").fill(key);
  await page.getByText("更换模型",{exact:true}).click();
  await page
    .getByRole("checkbox", { name: "启用 gpt-4.1", exact: true })
    .check();
  await page
    .getByRole("checkbox", { name: "启用 gpt-5.4", exact: true })
    .check();
  const chooseDefault = page.getByRole("button", {
    name: "将 gpt-5.4 设为默认模型",
  });
  if (await chooseDefault.count()) await chooseDefault.click();
  assert.deepEqual(
    (await new AxeBuilder({ page }).analyze()).violations.map((v) => v.id),
    [],
  );
  await page
    .getByRole("dialog")
    .screenshot({ path: "docs/screenshots/gpt-claude-protocol-form.png" });
  await page.getByRole("button", { name: "添加并使用", exact: true }).click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  let overview = await invoke("get_overview"),
    provider = overview.providers[0];
  assert.equal(provider.codexOptions.claudeProtocol, "openai");
  const desktop = overview.targets.find((t) => t.target === "claude_desktop");
  const profileFile = desktop.files.find(
    (f) => f.endsWith(".json") && f.includes("e82de475"),
  );
  assert.ok(profileFile, JSON.stringify(desktop));
  const profile = JSON.parse(await readFile(profileFile, "utf8"));
  assert.deepEqual(
    profile.inferenceModels.map((m) => m.labelOverride),
    ids,
  );
  assert.ok(
    profile.inferenceModels.every((m) =>
      m.name.startsWith("claude-sonnet-4-6-uni-"),
    ),
  );
  assert.ok(!JSON.stringify(profile).includes(key));
  const dHeaders = {
    "Content-Type": "application/json",
    Authorization: `Bearer ${profile.inferenceGatewayApiKey}`,
  };
  const dCall = (body) =>
    fetch(`${profile.inferenceGatewayBaseUrl}/v1/messages`, {
      method: "POST",
      headers: dHeaders,
      body: JSON.stringify(body),
    });
  const simple = {
    model: profile.inferenceModels[0].name,
    messages: [{ role: "user", content: "Hello" }],
    max_tokens: 8192,
  };
  assert.equal(
    (await fetch(`${profile.inferenceGatewayBaseUrl}/v1/models`)).status,
    401,
  );
  assert.equal(
    (
      await fetch(`${profile.inferenceGatewayBaseUrl}/v1/models`, {
        headers: { ...dHeaders, Origin: "https://other.test" },
      })
    ).status,
    401,
  );
  const json = await (await dCall(simple)).json();
  assert.equal(json.content[0].text, "GPT converted reply · 中文正常");
  assert.equal(requests.at(-1).body.model, "gpt-5.4");
  const second = await (
    await dCall({
      ...simple,
      model: profile.inferenceModels[1].name,
      stream: true,
    })
  ).text();
  assert.ok(second.includes("message_stop"));
  assert.equal(requests.at(-1).body.model, "gpt-4.1");
  const count = await fetch(
    `${profile.inferenceGatewayBaseUrl}/v1/messages/count_tokens`,
    { method: "POST", headers: dHeaders, body: JSON.stringify(simple) },
  );
  assert.equal(count.headers.get("x-uni-switch-token-count"), "estimated");
  assert.ok((await count.json()).input_tokens > 0);
  checks.push(
    "Native UI selects OpenAI for Claude, discovers GPT models, writes desktop aliases with real GPT display labels; JSON/SSE alias routing, authentication and count estimate work; zero Axe violations",
  );
  await invoke("apply_provider", {
    target: "claude_cli",
    providerId: provider.id,
  });
  const settings = JSON.parse(
    await readFile(path.join(cliDir, "settings.json"), "utf8"),
  );
  assert.equal(settings.env.ANTHROPIC_MODEL, "gpt-5.4");
  const headers = {
    "Content-Type": "application/json",
    Authorization: `Bearer ${settings.env.ANTHROPIC_AUTH_TOKEN}`,
  };
  const url = settings.env.ANTHROPIC_BASE_URL;
  const call = (body, extra = {}) =>
    fetch(`${url}/v1/messages`, {
      method: "POST",
      headers,
      body: JSON.stringify(body),
      ...extra,
    });
  const env = {
    ...process.env,
    CLAUDE_CONFIG_DIR: cliDir,
    DISABLE_NONESSENTIAL_TRAFFIC: "1",
    CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: "1",
    CLAUDE_CODE_DISABLE_AUTOUPDATER: "1",
    CLAUDE_CODE_SAFE_MODE: "1",
    NO_PROXY: "localhost,127.0.0.1",
  };
  for (const name of Object.keys(env))
    if (
      /^(ANTHROPIC_|OPENAI_API_KEY|CLAUDE_CODE_OAUTH_TOKEN)|^(https?|all)_proxy$/i.test(
        name,
      )
    )
      delete env[name];
  const binary =
    process.env.CLAUDE_QA_BIN ||
    "C:/Users/Administrator/AppData/Roaming/npm/node_modules/@anthropic-ai/claude-code/bin/claude.exe";
  async function runCli(prompt, model = "gpt-5.4", extra = []) {
    const task = promisify(execFile)(
      binary,
      [
        "-p",
        "--output-format",
        "stream-json",
        "--verbose",
        "--include-partial-messages",
        "--model",
        model,
        "--setting-sources",
        "user",
        "--disable-slash-commands",
        "--strict-mcp-config",
        "--mcp-config",
        '{"mcpServers":{}}',
        "--tools",
        "Read,Write",
        "--allowedTools",
        "Read,Write",
        "--permission-mode",
        "acceptEdits",
        ...extra,
        prompt,
      ],
      {
        env,
        cwd: work,
        windowsHide: true,
        timeout: 60000,
        maxBuffer: 16 * 1024 * 1024,
      },
    );
    task.child.stdin.end();
    try {
      const result = await task;
      await writeFile(
        path.join(root, `cli-${mode}-${Date.now()}.jsonl`),
        result.stdout,
      );
      assert.ok(
        result.stdout.includes("GPT converted reply"),
        JSON.stringify(result),
      );
      return result.stdout;
    } catch (e) {
      await writeFile(
        path.join(root, `cli-failed-${mode}.log`),
        `${e.stdout}\n${e.stderr}`,
      );
      throw e;
    }
  }
  console.log("QA: real Claude CLI text");
  mode = "text";
  let log = await runCli("Reply hello. Do not call tools.");
  const session = log
    .trim()
    .split("\n")
    .map((line) => JSON.parse(line))
    .find((m) => m.type === "result")?.session_id;
  assert.ok(session);
  console.log("QA: real Claude CLI read");
  mode = "read";
  await runCli("Read input.txt and report its contents.");
  assert.ok(
    toolResult.includes("GPT bridge real Claude tool read verified"),
    toolResult,
  );
  assert.ok(
    requests.some((r) =>
      r.body.input?.some(
        (i) => i.type === "reasoning" && i.encrypted_content === "encrypted_qa",
      ),
    ),
    "Encrypted reasoning must be replayed on tool continuation",
  );
  console.log("QA: real Claude CLI write");
  mode = "write";
  await runCli("Write created.txt with the requested content.");
  assert.equal(
    (await readFile(path.join(work, "created.txt"), "utf8")).trim(),
    "GPT bridge real Claude write verified.",
  );
  console.log("QA: real Claude CLI resume/model switch");
  mode = "text";
  await runCli(
    "Continue our conversation. Reply hello again without tools.",
    "gpt-4.1",
    ["--resume", session],
  );
  assert.equal(requests.at(-1).body.model, "gpt-4.1");
  checks.push(
    "Real Claude CLI 2.1.179 completes GPT text, executes Read and Write, returns tool results and encrypted reasoning, resumes a saved session and switches GPT model",
  );
  console.log("QA: real Claude CLI Chat fallback");
  mode = "chat-read";
  toolResult = "";
  await runCli("Read input.txt and report its contents.", "gpt-4.1");
  assert.ok(toolResult.includes("GPT bridge real Claude tool read verified"));
  checks.push(
    "Real Claude CLI also executes a complete GPT tool loop through Chat Completions fallback after Responses 404",
  );
  mode = "chat";
  const chat = await (await call({ ...simple, model: "gpt-4.1" })).json();
  assert.equal(chat.stop_reason, "end_turn");
  mode = "text";
  const stop = await (
    await call({ ...simple, model: "gpt-4.1", stop_sequences: ["DONE"] })
  ).json();
  assert.equal(stop.stop_reason, "end_turn");
  assert.equal(requests.at(-1).body.stop[0], "DONE");
  mode = "http-error";
  let before = requests.length;
  const rejected = await call({ ...simple, model: "gpt-5.4" });
  assert.equal(rejected.status, 429);
  assert.ok(!(await rejected.text()).includes(key));
  assert.equal(requests.length, before + 1);
  mode = "malformed";
  before = requests.length;
  assert.equal((await call({ ...simple, model: "gpt-5.4" })).status, 502);
  assert.equal(requests.length, before + 1);
  mode = "stream-error";
  const error = await (
    await call({ ...simple, model: "gpt-5.4", stream: true })
  ).text();
  assert.ok(error.includes('"type":"error"'));
  assert.ok(!error.includes(key));
  assert.ok(!error.includes("message_stop"));
  mode = "truncated";
  const truncated = await (
    await call({ ...simple, model: "gpt-5.4", stream: true })
  ).text();
  assert.ok(truncated.includes('"type":"error"'));
  assert.ok(!truncated.includes("message_stop"));
  mode = "cancel";
  const abort = new AbortController();
  const cancel = await call(
    { ...simple, model: "gpt-5.4", stream: true },
    { signal: abort.signal },
  );
  await cancel.body.getReader().read();
  abort.abort();
  await until(() => disconnected);
  checks.push(
    "JSON Chat fallback and stop sequences; no fallback on 429 or malformed success; redacted HTTP/SSE errors, truncated stream rejection and upstream cancellation",
  );
  mode = "text";
  const secondProvider = await invoke("save_provider", {
    input: {
      id: null,
      family: "claude",
      name: "Independent CLI GPT",
      baseUrl: origin,
      apiKey: key,
      model: "gpt-4.1",
      authMode: "bearer",
      reasoningEffort: null,
      codexOptions: { ...provider.codexOptions },
    },
  });
  await invoke("apply_provider", {
    target: "claude_cli",
    providerId: secondProvider.id,
  });
  assert.equal((await dCall(simple)).status, 200);
  assert.equal((await call({ ...simple, model: "gpt-5.4" })).status, 409);
  const newSettings = JSON.parse(
    await readFile(path.join(cliDir, "settings.json"), "utf8"),
  );
  const newCall = () =>
    fetch(`${newSettings.env.ANTHROPIC_BASE_URL}/v1/messages`, {
      method: "POST",
      headers,
      body: JSON.stringify({ ...simple, model: "gpt-4.1" }),
    });
  assert.equal((await newCall()).status, 200);
  await invoke("save_provider", {
    input: {
      id: secondProvider.id,
      family: "claude",
      name: secondProvider.name,
      baseUrl: origin,
      apiKey: "unapplied-key",
      model: "gpt-4.1",
      authMode: "bearer",
      reasoningEffort: null,
      codexOptions: secondProvider.codexOptions,
    },
  });
  assert.equal((await newCall()).status, 200);
  await promisify(execFile)(
    "powershell.exe",
    [
      "-NoProfile",
      "-Command",
      `(Get-Process -Id ${child.pid}).CloseMainWindow()`,
    ],
    { windowsHide: true },
  );
  await new Promise((r) => setTimeout(r, 300));
  assert.equal(child.exitCode, null);
  assert.equal((await newCall()).status, 200);
  await browser.close();
  browser = null;
  await new Promise((r) => {
    child.once("exit", r);
    child.kill();
  });
  await until(async () => {
    try {
      return !(await fetch("http://127.0.0.1:9223/json/version")).ok;
    } catch {
      return true;
    }
  });
  child = spawn(exe, [], { windowsHide: true, stdio: "ignore", env: appEnv });
  await until(async () => {
    try {
      return (await fetch("http://127.0.0.1:9223/json/version")).ok;
    } catch {
      return false;
    }
  });
  browser = await until(async () => {
    try {
      return await chromium.connectOverCDP("http://127.0.0.1:9223");
    } catch {
      return null;
    }
  });
  page = browser.contexts()[0].pages()[0];
  await page.getByRole("button", { name: /^添加(第一个)?供应商$/ }).waitFor();
  assert.equal((await newCall()).status, 200);
  assert.equal((await dCall(simple)).status, 200);
  await invoke("restore_original", { target: "claude_cli" });
  assert.equal((await newCall()).status, 409);
  assert.equal((await dCall(simple)).status, 200);
  const restored = JSON.parse(
    await readFile(path.join(cliDir, "settings.json"), "utf8"),
  );
  assert.equal(restored.env.ENABLE_TOOL_SEARCH, "auto:10");
  assert.equal(restored.env.CUSTOM_QA, "preserve");
  await invoke("restore_original", { target: "claude_desktop" });
  assert.equal((await dCall(simple)).status, 409);
  checks.push(
    "Independent desktop/CLI suppliers, unapplied credential isolation, tray lifetime, persistent routes after restart and transactional restore of CLI settings and both routes",
  );
  await writeFile(
    path.join(root, "result.json"),
    JSON.stringify(
      {
        checks,
        requests,
        binary,
        limitation:
          "Real Claude CLI and native uni-switch against isolated fake OpenAI suppliers; desktop profile and routing verified, desktop inference UI not exercised; no real user keys or client configs used.",
      },
      null,
      2,
    ),
  );
  console.log(JSON.stringify({ root, checks }, null, 2));
} catch (e) {
  await writeFile(
    path.join(root, "requests.json"),
    JSON.stringify(requests, null, 2),
  );
  if (page) {
    console.error(
      await page
        .locator("body")
        .innerText()
        .catch(() => "unavailable"),
    );
    await page
      .screenshot({ path: path.join(root, "failure.png") })
      .catch(() => {});
  }
  throw e;
} finally {
  if (browser) await browser.close();
  if (child) child.kill();
  server.closeAllConnections();
  await new Promise((r) => server.close(r));
}
