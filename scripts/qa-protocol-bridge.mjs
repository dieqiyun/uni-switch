import { chromium } from "playwright";
import AxeBuilder from "@axe-core/playwright";
import http from "node:http";
import { spawn, execFile } from "node:child_process";
import { promisify } from "node:util";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import assert from "node:assert/strict";

const root = path.resolve(".qa/protocol-bridge/native", String(Date.now()));
const codexHome = path.join(root, "codex");
await mkdir(codexHome, { recursive: true });
await writeFile(
  path.join(codexHome, "bridge-input.txt"),
  "Claude bridge tool read verified.\n",
  "utf8",
);
const key = "fake-anthropic-key-only-for-qa";
const requests = [],
  checks = [];
let mode = "text",
  toolResult = "",
  disconnected = false;
const modelIds = ["claude-opus-4-6", "claude-sonnet-4-6"];
function emit(res, type, body) {
  res.write(`event: ${type}\ndata: ${JSON.stringify({ type, ...body })}\n\n`);
}
const server = http.createServer(async (req, res) => {
  if (
    req.headers["x-api-key"] !== key ||
    req.headers["anthropic-version"] !== "2023-06-01"
  ) {
    res.writeHead(401, { "Content-Type": "application/json" });
    res.end(
      JSON.stringify({
        error: { message: "Missing Anthropic authentication" },
      }),
    );
    return;
  }
  if (req.url === "/v1/models") {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(
      JSON.stringify({ data: modelIds.map((id) => ({ id })), has_more: false }),
    );
    return;
  }
  if (req.url !== "/v1/messages") {
    res.writeHead(404);
    res.end("{}");
    return;
  }
  let raw = "";
  for await (const chunk of req) raw += chunk;
  const body = JSON.parse(raw);
  requests.push(body);
  if (mode === "http-error") {
    res.writeHead(429, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ error: { message: `rate limit ${key}` } }));
    return;
  }
  if (mode === "cancel") {
    res.writeHead(200, { "Content-Type": "text/event-stream" });
    emit(res, "message_start", {
      message: {
        id: "msg_cancel",
        role: "assistant",
        content: [],
        usage: { input_tokens: 10, output_tokens: 0 },
      },
    });
    const timer = setInterval(() => emit(res, "ping", {}), 100);
    res.on("close", () => {
      disconnected = true;
      clearInterval(timer);
    });
    return;
  }
  let blocks = [{ type: "text", text: "Claude converted reply · 中文正常。" }],
    stop = "end_turn";
  const callId =
    mode === "read"
      ? "tool_read"
      : mode === "patch"
        ? "tool_patch"
        : mode === "custom"
          ? "tool_custom"
          : "";
  const result = body.messages
    .flatMap((m) => m.content)
    .findLast((b) => b.type === "tool_result" && b.tool_use_id === callId);
  if (result) {
    toolResult = JSON.stringify(result.content);
    blocks = [{ type: "text", text: `${mode} tool result received.` }];
  } else if (mode === "read") {
    const tool = body.tools.find((t) =>
      /shell_command$|exec_command$/.test(t.name),
    );
    assert.ok(tool, `Missing shell tool: ${JSON.stringify(body.tools)}`);
    const name = Object.hasOwn(tool.input_schema.properties || {}, "command")
      ? "command"
      : "cmd";
    blocks = [
      {
        type: "thinking",
        thinking: "Read the isolated fixture.",
        signature: "qa-signature",
      },
      {
        type: "tool_use",
        id: callId,
        name: tool.name,
        input: {
          [name]: "Get-Content -Raw -Encoding UTF8 './bridge-input.txt'",
          workdir: codexHome,
        },
      },
    ];
    stop = "tool_use";
  } else if (mode === "patch" || mode === "custom") {
    const tool = body.tools.find((t) => /apply_patch$/.test(t.name));
    assert.ok(tool, `Missing patch tool: ${JSON.stringify(body.tools)}`);
    const patch =
      "*** Begin Patch\n*** Add File: bridge-created.txt\n+Claude patch converted successfully.\n*** End Patch";
    const fields = tool.input_schema.properties || {};
    const field = Object.hasOwn(fields, "patch") ? "patch" : "input";
    blocks = [
      {
        type: "tool_use",
        id: callId,
        name: tool.name,
        input: { [field]: patch },
      },
    ];
    stop = "tool_use";
  }
  if (!body.stream) {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(
      JSON.stringify({
        id: "msg_json",
        type: "message",
        role: "assistant",
        model: body.model,
        content: blocks,
        stop_reason: stop,
        usage: { input_tokens: 20, output_tokens: 5 },
      }),
    );
    return;
  }
  res.writeHead(200, { "Content-Type": "text/event-stream" });
  emit(res, "message_start", {
    message: {
      id: "msg_mock",
      type: "message",
      role: "assistant",
      model: body.model,
      content: [],
      usage: { input_tokens: 20, output_tokens: 0, cache_read_input_tokens: 3 },
    },
  });
  if (mode === "stream-error") {
    emit(res, "error", {
      error: { type: "overloaded_error", message: `overloaded ${key}` },
    });
    res.end();
    return;
  }
  for (const [index, block] of blocks.entries()) {
    const start =
      block.type === "tool_use"
        ? { ...block, input: {} }
        : block.type === "thinking"
          ? { ...block, thinking: "", signature: "" }
          : { ...block, text: "" };
    emit(res, "content_block_start", { index, content_block: start });
    if (block.type === "tool_use") {
      const input = JSON.stringify(block.input);
      for (let i = 0; i < input.length; i += 7)
        emit(res, "content_block_delta", {
          index,
          delta: {
            type: "input_json_delta",
            partial_json: input.slice(i, i + 7),
          },
        });
    } else if (block.type === "thinking") {
      emit(res, "content_block_delta", {
        index,
        delta: { type: "thinking_delta", thinking: block.thinking },
      });
      emit(res, "content_block_delta", {
        index,
        delta: { type: "signature_delta", signature: block.signature },
      });
    } else {
      for (const text of [block.text.slice(0, 8), block.text.slice(8)])
        emit(res, "content_block_delta", {
          index,
          delta: { type: "text_delta", text },
        });
    }
    emit(res, "content_block_stop", { index });
  }
  emit(res, "message_delta", {
    delta: { stop_reason: stop, stop_sequence: null },
    usage: { output_tokens: 5 },
  });
  if (mode !== "truncated") emit(res, "message_stop", {});
  res.end();
});
await new Promise((r) => server.listen(0, "127.0.0.1", r));
const origin = `http://127.0.0.1:${server.address().port}`;
async function until(fn, timeout = 25000) {
  const end = Date.now() + timeout;
  while (Date.now() < end) {
    const v = await fn();
    if (v) return v;
    await new Promise((r) => setTimeout(r, 100));
  }
  throw Error("QA timeout");
}
let child, browser, page, codex;
try {
  try {
    assert.equal((await fetch("http://127.0.0.1:9223/json/version")).ok, false);
  } catch (e) {
    if (e.code === "ERR_ASSERTION") throw e;
  }
  child = spawn(
    path.resolve(
      process.env.UNI_SWITCH_QA_EXE ||
        ".qa/protocol-bridge/test-app/uni-switch.exe",
    ),
    [],
    {
      windowsHide: true,
      stdio: "ignore",
      env: {
        ...process.env,
        UNI_SWITCH_DATA_DIR: path.join(root, "data"),
        WEBVIEW2_USER_DATA_FOLDER: path.join(root, "webview"),
        CODEX_HOME: codexHome,
        CLAUDE_CONFIG_DIR: path.join(root, "claude-cli"),
        LOCALAPPDATA: path.join(root, "local"),
      },
    },
  );
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
  await page.getByRole("button", { name: /^添加(第一个)?供应商$/ }).waitFor();
  await invoke("set_directory", {target:"codex", directory:codexHome});
  await page.getByRole("button", { name: /^添加(第一个)?供应商$/ }).click();
  await page.locator(".form-advanced > summary").click();
  await page.getByLabel("API 协议").selectOption("anthropic");
  await page.locator("#baseUrl").fill(origin);
  await page.locator("#apiKey").fill(key);
  await page.getByText("更换模型",{exact:true}).click();
  await page
    .getByRole("checkbox", { name: "启用 claude-sonnet-4-6", exact: true })
    .waitFor();
  await page
    .getByRole("checkbox", { name: "启用 claude-sonnet-4-6", exact: true })
    .check();
  await page.getByRole("checkbox", { name: "启用 claude-opus-4-6", exact: true }).check();
  await page
    .getByRole("dialog")
    .screenshot({ path: "docs/screenshots/claude-protocol-form.png" });
  const audit = await new AxeBuilder({ page }).analyze();
  assert.deepEqual(
    audit.violations.map((v) => v.id),
    [],
  );
  await page.getByRole("button", { name: "添加并使用", exact: true }).click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  const overview = await invoke("get_overview");
  const provider = overview.providers[0];
  assert.equal(provider.codexOptions.protocol, "anthropic");
  assert.equal(provider.authMode, "x-api-key");
  const config = await readFile(path.join(codexHome, "config.toml"), "utf8");
  const url = config.match(/base_url = "([^"]+)"/)[1];
  const token = config.match(/experimental_bearer_token = "([^"]+)"/)[1];
  assert.ok(url.startsWith("http://127.0.0.1:"));
  assert.ok(!config.includes(key));
  const headers = {
    "Content-Type": "application/json",
    Authorization: `Bearer ${token}`,
  };
  const call = (body, extra = {}) =>
    fetch(`${url}/responses`, {
      method: "POST",
      headers,
      body: JSON.stringify(body),
      ...extra,
    });
  assert.equal((await fetch(`${url}/models`)).status, 401);
  assert.equal(
    (
      await fetch(`${url}/models`, {
        headers: { ...headers, Origin: "https://other.test" },
      })
    ).status,
    401,
  );
  const list = await (await fetch(`${url}/models`, { headers })).json();
  assert.deepEqual(
    list.data.map((m) => m.id),
    modelIds,
  );
  const text = await (
    await call({ model: provider.model, input: "Hello", stream: true })
  ).text();
  assert.ok(text.includes("response.completed"));
  assert.ok(text.includes("中文正常"));
  const json = await (
    await call({ model: provider.model, input: "Hello", stream: false })
  ).json();
  assert.equal(json.status, "completed");
  assert.equal(json.output[0].type, "message");
  checks.push(
    "UI selects Claude protocol, discovers both models, writes a loopback route and separate token; zero Axe violations; JSON and SSE text work",
  );

  const binary =
    process.env.CODEX_QA_BIN ||
    "C:/Users/Administrator/AppData/Local/OpenAI/Codex/bin/8aaf1547b825b104/codex.exe";
  const env = {
    ...process.env,
    CODEX_HOME: codexHome,
    NO_PROXY: "localhost,127.0.0.1",
  };
  delete env.OPENAI_API_KEY;
  for (const name of Object.keys(env))
    if (/^(https?|all)_proxy$/i.test(name)) delete env[name];
  codex = spawn(binary, ["app-server"], {
    env,
    cwd: codexHome,
    windowsHide: true,
  });
  let buffer = "",
    stderr = "",
    seq = 0;
  const pending = new Map(),
    notifications = [];
  codex.stderr.on("data", (c) => (stderr += c));
  codex.stdout.on("data", (c) => {
    buffer += c;
    const lines = buffer.split("\n");
    buffer = lines.pop();
    for (const line of lines) {
      let m;
      try {
        m = JSON.parse(line);
      } catch {
        continue;
      }
      if (m.id != null) {
        const p = pending.get(m.id);
        if (p) {
          pending.delete(m.id);
          m.error
            ? p.reject(Error(JSON.stringify(m.error)))
            : p.resolve(m.result);
        }
      } else notifications.push(m);
    }
  });
  const rpc = (method, params) =>
    new Promise((resolve, reject) => {
      const id = ++seq;
      pending.set(id, { resolve, reject });
      codex.stdin.write(JSON.stringify({ id, method, params }) + "\n");
    });
  await rpc("initialize", {
    clientInfo: {
      name: "uni_bridge_qa",
      title: "Protocol bridge QA",
      version: "0.3.7",
    },
    capabilities: { experimentalApi: true },
  });
  codex.stdin.write(JSON.stringify({ method: "initialized" }) + "\n");
  const models = await rpc("model/list", { limit: 100, includeHidden: false });
  assert.deepEqual(models.data.map((m) => m.id).sort(), modelIds);
  const thread = await rpc("thread/start", {
    model: provider.model,
    cwd: codexHome,
    ephemeral: true,
    sandbox: "danger-full-access",
    approvalPolicy: "never",
  });
  async function turn(text, model = provider.model) {
    const start = notifications.length;
    const result = await rpc("turn/start", {
      threadId: thread.thread.id,
      model,
      input: [{ type: "text", text }],
    });
    const completed = await until(
      () =>
        notifications
          .slice(start)
          .find(
            (n) =>
              n.method === "turn/completed" &&
              n.params.turn.id === result.turn.id,
          ),
      45000,
    );
    assert.equal(
      completed.params.turn.status,
      "completed",
      JSON.stringify({ completed, stderr }),
    );
    return notifications.slice(start);
  }
  console.log("QA: real Codex text");
  mode = "text";
  await turn("Reply hello. Do not use tools.");
  console.log("QA: real Codex read tool");
  mode = "read";
  await turn("Read bridge-input.txt using the shell tool and report its text.");
  assert.ok(
    toolResult.includes("Claude bridge tool read verified"),
    toolResult,
  );
  assert.ok(
    requests.some((r) =>
      r.messages.some((m) =>
        m.content.some(
          (b) => b.type === "thinking" && b.signature === "qa-signature",
        ),
      ),
    ),
    "Signed thinking must be returned in tool continuation",
  );
  console.log("QA: real Codex patch tool");
  mode = "patch";
  await turn("Create bridge-created.txt using apply_patch.");
  assert.equal(
    (await readFile(path.join(codexHome, "bridge-created.txt"), "utf8")).trim(),
    "Claude patch converted successfully.",
  );
  console.log("QA: model switch");
  mode = "text";
  await turn("Reply hello again without tools.", "claude-sonnet-4-6");
  assert.equal(requests.at(-1).model, "claude-sonnet-4-6");
  console.log("QA: real Codex compaction");
  const compactStart = notifications.length;
  await rpc("thread/compact/start", { threadId: thread.thread.id });
  await until(
    () =>
      notifications
        .slice(compactStart)
        .some(
          (n) =>
            n.method === "thread/compacted" ||
            (n.method === "item/completed" &&
              n.params.item.type === "contextCompaction"),
        ),
    45000,
  );
  await turn("Continue after the summary. Reply hello without tools.");
  checks.push(
    "Real Codex 0.160 app-server loads both Claude IDs, completes five turns and compaction, executes shell read and custom apply_patch, sends results and signed thinking back, and switches model in the same thread",
  );
  console.log("QA: real Codex CLI with reasoning");
  await invoke("repair_reasoning_levels", { providerId: provider.id });
  assert.equal(
    (await invoke("get_overview")).targets.find((t) => t.target === "codex")
      .state,
    "applied",
  );
  const cliTask = promisify(execFile)(
    binary,
    [
      "exec",
      "--skip-git-repo-check",
      "--json",
      "-m",
      "claude-sonnet-4-6",
      "-c",
      'model_reasoning_effort="high"',
      "Reply hello without tools.",
    ],
    {
      env,
      cwd: codexHome,
      windowsHide: true,
      timeout: 45000,
      maxBuffer: 8 * 1024 * 1024,
    },
  );
  cliTask.child.stdin.end();
  const cli = await cliTask;
  assert.ok(cli.stdout.includes("Claude converted reply"));
  assert.equal(requests.at(-1).thinking.type, "adaptive");
  assert.equal(requests.at(-1).output_config.effort, "high");
  checks.push(
    "Real Codex CLI exec completes a Claude reply and maps high reasoning to Anthropic adaptive thinking with high effort",
  );

  mode = "text";
  const compact = await (
    await fetch(`${url}/responses/compact`, {
      method: "POST",
      headers,
      body: JSON.stringify({
        model: provider.model,
        input: [{ role: "user", content: "Remember the file changes" }],
      }),
    })
  ).json();
  assert.equal(compact.object, "response.compaction");
  await call({
    model: provider.model,
    input: [...compact.output, { role: "user", content: "Continue" }],
    stream: false,
  });
  assert.ok(
    requests
      .at(-1)
      .messages[0].content[0].text.includes("Previous conversation summary"),
  );
  mode = "custom";
  const custom = await (
    await call({
      model: provider.model,
      input: "Patch",
      stream: true,
      tools: [
        { type: "custom", name: "apply_patch", description: "patch format" },
      ],
    })
  ).text();
  assert.ok(custom.includes("custom_tool_call"));
  assert.ok(custom.includes("*** Begin Patch"));
  mode = "http-error";
  const rejected = await call({
    model: provider.model,
    input: "Hi",
    stream: true,
  });
  assert.equal(rejected.status, 429);
  assert.ok(!(await rejected.text()).includes(key));
  mode = "stream-error";
  const streamed = await (
    await call({ model: provider.model, input: "Hi", stream: true })
  ).text();
  assert.ok(streamed.includes("anthropic_stream_error"));
  assert.ok(!streamed.includes(key));
  assert.ok(!streamed.includes("response.completed"));
  mode = "truncated";
  const truncated = await (
    await call({ model: provider.model, input: "Hi", stream: true })
  ).text();
  assert.ok(truncated.includes("message_stop"));
  assert.ok(!truncated.includes("response.completed"));
  mode = "cancel";
  const abort = new AbortController();
  const cancel = await call(
    { model: provider.model, input: "Hi", stream: true },
    { signal: abort.signal },
  );
  const reader = cancel.body.getReader();
  await reader.read();
  abort.abort();
  await until(() => disconnected);
  checks.push(
    "Compaction summary replay, custom tool stream, sanitized 429/stream errors, truncated stream rejection and upstream cancellation",
  );

  mode = "text";
  console.log("QA: tray lifetime and restart");
  const closed = await promisify(execFile)(
    "powershell.exe",
    [
      "-NoProfile",
      "-Command",
      `(Get-Process -Id ${child.pid}).CloseMainWindow()`,
    ],
    { windowsHide: true },
  );
  assert.ok(closed.stdout.includes("True"));
  await new Promise((r) => setTimeout(r, 300));
  assert.equal(
    child.exitCode,
    null,
    "Closing the window must leave the bridge running",
  );
  assert.equal(
    (
      await call({
        model: provider.model,
        input: "Hi after closing window",
        stream: false,
      })
    ).status,
    200,
  );
  const savedEnv = child.spawnargs;
  await browser.close();
  browser = null;
  await new Promise((resolve) => {
    child.once("exit", resolve);
    child.kill();
  });
  await until(async () => {
    try {
      return !(await fetch("http://127.0.0.1:9223/json/version")).ok;
    } catch {
      return true;
    }
  });
  child = spawn(savedEnv[0], [], {
    windowsHide: true,
    stdio: "ignore",
    env: {
      ...process.env,
      UNI_SWITCH_DATA_DIR: path.join(root, "data"),
      WEBVIEW2_USER_DATA_FOLDER: path.join(root, "webview"),
      CODEX_HOME: codexHome,
      CLAUDE_CONFIG_DIR: path.join(root, "claude-cli"),
      LOCALAPPDATA: path.join(root, "local"),
    },
  });
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
  assert.equal(
    (
      await call({
        model: provider.model,
        input: "Hi after restart",
        stream: false,
      })
    ).status,
    200,
    "Persistent port and local token must work after restarting uni-switch without rewriting Codex",
  );
  checks.push(
    "Closing the main window leaves the bridge running in tray; restarting uni-switch retains the port, token and applied route without reapplying Codex config",
  );
  await invoke("save_provider", {
    input: {
      id: provider.id,
      family: "codex",
      name: provider.name,
      baseUrl: provider.baseUrl,
      apiKey: "unapplied-key",
      model: provider.model,
      authMode: "x-api-key",
      reasoningEffort: null,
      codexOptions: provider.codexOptions,
    },
  });
  assert.equal(
    (await call({ model: provider.model, input: "Hi", stream: false })).status,
    200,
    "Unapplied credentials must not affect active connection",
  );
  await invoke("restore_original", { target: "codex" });
  assert.equal(
    (await call({ model: provider.model, input: "Hi", stream: false })).status,
    409,
  );
  checks.push(
    "Unapplied credential edits leave active requests on the applied snapshot; restoring original config deactivates bridge route",
  );
  await writeFile(
    path.join(root, "result.json"),
    JSON.stringify(
      {
        checks,
        requests,
        notifications,
        binary,
        limitation:
          "Real Codex runtime and native uni-switch against an isolated fake Anthropic supplier; no real supplier credentials or user configuration used.",
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
        .catch(() => "Page unavailable"),
    );
    await page
      .screenshot({ path: path.join(root, "failure.png") })
      .catch(() => {});
  }
  throw e;
} finally {
  if (codex) codex.kill();
  if (browser) await browser.close();
  if (child) child.kill();
  server.closeAllConnections();
  await new Promise((r) => server.close(r));
}
