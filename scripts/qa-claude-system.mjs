import assert from "node:assert/strict";
import http from "node:http";
import net from "node:net";
import path from "node:path";
import { spawn } from "node:child_process";
import { copyFile, mkdir, readFile, writeFile } from "node:fs/promises";
import { chromium } from "playwright";
import { preview } from "vite";

const root = path.resolve(".qa/claude-system", String(Date.now()));
const home = path.join(root, "home"),
  cli = path.join(home, ".claude");
const local = path.join(root, "Local"),
  roaming = path.join(root, "Roaming");
await Promise.all(
  [cli, local, roaming].map((dir) => mkdir(dir, { recursive: true })),
);
const model = "claude-opus-5-5",
  key = "synthetic-system-upstream-key";
const marker = "Current system instructions: 中文",
  reply = "Claude system bridge OK.";
const legacy = process.env.UNI_SWITCH_QA_EXPECT_LEGACY_ROLE_ERROR === "1";
const requests = [],
  relayed = [],
  checks = [],
  pageErrors = [];
let bridgeUrl,
  injectSystem = true;
const upstream = http.createServer(async (req, res) => {
  let raw = "";
  for await (const chunk of req) raw += chunk;
  const body = raw ? JSON.parse(raw) : {};
  const requestPath = new URL(req.url, "http://127.0.0.1").pathname;
  requests.push({ path: requestPath, body });
  res.setHeader("content-type", "application/json");
  if (req.headers.authorization !== "Bearer " + key) {
    res.statusCode = 401;
    return res.end(
      JSON.stringify({ error: { message: "Synthetic authentication failed" } }),
    );
  }
  if (requestPath === "/v1/models")
    return res.end(JSON.stringify({ data: [{ id: model }] }));
  const chat = requestPath === "/v1/chat/completions";
  if (!chat && requestPath !== "/v1/responses") {
    res.statusCode = 404;
    return res.end("{}");
  }
  const response = {
    status: "completed",
    output: [
      { type: "message", content: [{ type: "output_text", text: reply }] },
    ],
  };
  if (!body.stream)
    return res.end(
      JSON.stringify(
        chat
          ? {
              choices: [
                {
                  message: { role: "assistant", content: reply },
                  finish_reason: "stop",
                },
              ],
            }
          : response,
      ),
    );
  res.setHeader("content-type", "text/event-stream");
  const events = chat
    ? [{ choices: [{ delta: { content: reply }, finish_reason: "stop" }] }]
    : [
        {
          type: "response.output_text.delta",
          output_index: 0,
          content_index: 0,
          delta: reply,
        },
        { type: "response.completed", response },
      ];
  for (const event of events)
    res.write("data: " + JSON.stringify(event) + "\n\n");
  if (chat) res.write("data: [DONE]\n\n");
  res.end();
});
await new Promise((resolve) => upstream.listen(0, "127.0.0.1", resolve));
const origin = "http://127.0.0.1:" + upstream.address().port;

// Replay actual CLI requests with a documented new-protocol system message.
// A first print turn in the installed CLI does not necessarily emit this role.
const relay = http.createServer(async (req, res) => {
  try {
    if (!req.url.includes("/messages")) {
      res.writeHead(404);
      return res.end("{}");
    }
    let raw = "";
    for await (const chunk of req) raw += chunk;
    const body = JSON.parse(raw);
    if (injectSystem)
      body.messages.push({
        role: "system",
        content: [{ type: "text", text: marker }],
        clear_at: "next_user_message",
        output_config: { effort: "low" },
      });
    relayed.push({
      path: req.url,
      injected: injectSystem,
      roles: body.messages.map((message) => message.role),
    });
    const headers = { "content-type": "application/json" };
    for (const name of [
      "authorization",
      "x-api-key",
      "anthropic-version",
      "anthropic-beta",
    ])
      if (req.headers[name]) headers[name] = req.headers[name];
    const response = await fetch(bridgeUrl + req.url, {
      method: "POST",
      headers,
      body: JSON.stringify(body),
    });
    res.writeHead(response.status, {
      "content-type":
        response.headers.get("content-type") ?? "application/json",
    });
    for await (const chunk of response.body) res.write(chunk);
    res.end();
  } catch {
    res.writeHead(502);
    res.end(JSON.stringify({ error: { message: "Isolated relay failed" } }));
  }
});
await new Promise((resolve) => relay.listen(0, "127.0.0.1", resolve));
const relayUrl = "http://127.0.0.1:" + relay.address().port;
let app,
  browser,
  page,
  previewServer,
  appOutput = "";
const portOpen = () =>
  new Promise((resolve) => {
    const socket = net.connect({ host: "127.0.0.1", port: 9223 });
    socket.once("connect", () => {
      socket.destroy();
      resolve(true);
    });
    socket.once("error", () => {
      socket.destroy();
      resolve(false);
    });
  });
const invoke = (command, args) =>
  page.evaluate(
    ({ command, args }) => window.__TAURI_INTERNALS__.invoke(command, args),
    { command, args },
  );
async function runClaude(settings, label) {
  const settingsPath = path.join(root, label + "-settings.json");
  const replaySettings = {
    ...settings,
    env: { ...settings.env, ANTHROPIC_BASE_URL: relayUrl },
  };
  await writeFile(settingsPath, JSON.stringify(replaySettings), "utf8");
  const env = { ...process.env };
  for (const name of Object.keys(env))
    if (
      /^(ANTHROPIC_|CLAUDE_|AWS_|GOOGLE_|VERTEX_|FOUNDRY_|OPENAI_)|^(https?|all|no)_proxy$/i.test(
        name,
      )
    )
      delete env[name];
  Object.assign(env, {
    HOME: home,
    USERPROFILE: home,
    APPDATA: roaming,
    LOCALAPPDATA: local,
    TEMP: root,
    TMP: root,
    CLAUDE_CONFIG_DIR: path.join(root, label + "-claude-config"),
    CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: "1",
    CLAUDE_CODE_SKIP_PROMPT_HISTORY: "1",
    DISABLE_TELEMETRY: "1",
    DISABLE_ERROR_REPORTING: "1",
    DISABLE_AUTOUPDATER: "1",
    NO_PROXY: "127.0.0.1,localhost",
    ...settings.env,
    ANTHROPIC_BASE_URL: relayUrl,
  });
  assert.ok(
    process.env.UNI_SWITCH_CLAUDE_BIN,
    "Set UNI_SWITCH_CLAUDE_BIN to the installed Claude executable",
  );
  const child = spawn(
    process.env.UNI_SWITCH_CLAUDE_BIN,
    [
      "--bare",
      "--print",
      "--model",
      model,
      "--tools",
      "",
      "--no-session-persistence",
      "--disable-slash-commands",
      "--strict-mcp-config",
      "--mcp-config",
      '{"mcpServers":{}}',
      "--setting-sources",
      "",
      "--settings",
      settingsPath,
      "Reply with one short greeting.",
    ],
    { cwd: home, env, windowsHide: true, stdio: "pipe" },
  );
  child.stdin.end();
  let output = "";
  child.stdout.on("data", (chunk) => (output += chunk));
  child.stderr.on("data", (chunk) => (output += chunk));
  const timer = setTimeout(() => child.kill(), 40000);
  try {
    const code = await new Promise((resolve, reject) => {
      child.once("error", reject);
      child.once("exit", resolve);
    });
    await writeFile(path.join(root, label + "-cli.log"), output, "utf8");
    return { code, output };
  } finally {
    clearTimeout(timer);
    if (child.exitCode === null && child.signalCode === null) child.kill();
  }
}

try {
  assert.equal(
    await portOpen(),
    false,
    "Existing QA/CDP must remain untouched",
  );
  previewServer = await preview({
    root: path.resolve("."),
    preview: { host: "127.0.0.1", port: 1420, strictPort: true },
    clearScreen: false,
  });
  const executable = path.join(root, "uni-switch.exe");
  await copyFile(
    path.resolve(
      process.env.UNI_SWITCH_QA_EXE ?? "src-tauri/target/debug/uni-switch.exe",
    ),
    executable,
  );
  const context = path.join(root, "context.json");
  await writeFile(
    context,
    JSON.stringify({
      home,
      local,
      roaming,
      roots: [home],
      notes: [],
      hints: [],
    }),
    "utf8",
  );
  app = spawn(executable, [], {
    windowsHide: true,
    stdio: "pipe",
    env: {
      ...process.env,
      HOME: home,
      APPDATA: roaming,
      LOCALAPPDATA: local,
      CODEX_HOME: path.join(home, ".codex"),
      CLAUDE_CONFIG_DIR: cli,
      UNI_SWITCH_DATA_DIR: path.join(root, "data"),
      UNI_SWITCH_QA_DISCOVERY_CONTEXT: context,
      WEBVIEW2_USER_DATA_FOLDER: path.join(root, "webview"),
      UNI_SWITCH_QA_UPDATE_REPOSITORY: "example/uni-switch",
      UNI_SWITCH_QA_UPDATE_ENDPOINT: origin + "/github/releases/latest",
    },
  });
  app.stdin.end();
  app.stdout.on("data", (chunk) => (appOutput += chunk));
  app.stderr.on("data", (chunk) => (appOutput += chunk));
  const deadline = Date.now() + 60000;
  while (!(await portOpen())) {
    assert.ok(Date.now() < deadline, "QA startup timed out: " + appOutput);
    assert.equal(app.exitCode, null, "QA app exited: " + appOutput);
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  browser = await chromium.connectOverCDP("http://127.0.0.1:9223");
  page = browser.contexts()[0].pages()[0];
  page.on("pageerror", (error) => pageErrors.push(error.message));
  await page.getByRole("button", { name: "设置", exact: true }).waitFor();
  await invoke("set_directory", { target: "claude_cli", directory: cli });
  for (const chat of legacy ? [false] : [false, true]) {
    const provider = await invoke("save_provider", {
      input: {
        id: null,
        family: "claude",
        name: "Synthetic system " + (chat ? "Chat" : "Responses"),
        baseUrl: origin + "/v1",
        apiKey: key,
        model,
        authMode: "bearer",
        reasoningEffort: null,
        codexOptions: {
          upstreamProtocol: "openai",
          claudeProtocol: "openai",
          models: [
            {
              id: model,
              enabled: true,
              profileOverrides: {
                endpoints: { responses: !chat, chatCompletions: chat },
                thinkingFormat: "openai",
                reasoningEfforts: ["low", "medium", "high"],
              },
            },
          ],
        },
      },
    });
    await invoke("apply_provider", {
      target: "claude_cli",
      providerId: provider.id,
    });
    const settings = JSON.parse(
      await readFile(path.join(cli, "settings.json"), "utf8"),
    );
    bridgeUrl = settings.env.ANTHROPIC_BASE_URL;
    assert.ok(bridgeUrl.startsWith("http://127.0.0.1:"));
    if (legacy) {
      const result = await runClaude(settings, "before");
      assert.notEqual(result.code, 0, result.output);
      assert.ok(
        result.output.includes("Claude 消息 role 必须为 user 或 assistant"),
        result.output,
      );
      assert.equal(
        requests.filter((request) => /responses|completions/.test(request.path))
          .length,
        0,
      );
      checks.push(
        "Old native bridge reproduces the exact screenshot 400 before upstream inference",
      );
      continue;
    }
    const source = {
      model,
      max_tokens: 256,
      system: "Initial instructions",
      thinking: { type: "adaptive" },
      messages: [
        { role: "user", content: "Previous turn" },
        { role: "assistant", content: "Previous reply" },
        { role: "user", content: "Current turn" },
        {
          role: "system",
          content: [{ type: "text", text: marker }],
          clear_at: "next_user_message",
          output_config: { effort: "low" },
        },
      ],
    };
    const call = (suffix, body) =>
      fetch(bridgeUrl + suffix, {
        method: "POST",
        headers: {
          "content-type": "application/json",
          authorization: "Bearer " + settings.env.ANTHROPIC_AUTH_TOKEN,
        },
        body: JSON.stringify(body),
      });
    const beforeCount = requests.length,
      count = await call("/v1/messages/count_tokens?beta=true", source);
    assert.equal(count.status, 200);
    assert.equal(count.headers.get("x-uni-switch-token-count"), "estimated");
    assert.ok((await count.json()).input_tokens > 0);
    assert.equal(requests.length, beforeCount);
    for (const stream of [false, true]) {
      const response = await call("/v1/messages?beta=true", {
        ...source,
        stream,
      });
      assert.equal(response.status, 200);
      const text = await response.text();
      assert.ok(text.includes(reply), text);
      assert.ok(text.includes(stream ? "message_stop" : "end_turn"), text);
      const body = requests.at(-1).body,
        history = chat ? body.messages : body.input;
      assert.equal(history.at(-1).role, "system");
      assert.equal(history.at(-1).content[0].text, marker);
      assert.equal(chat ? body.reasoning_effort : body.reasoning.effort, "low");
      assert.equal(
        chat ? history[0].content : body.instructions,
        "Initial instructions",
      );
    }
    for (const injected of [false, true]) {
      injectSystem = injected;
      const result = await runClaude(
        settings,
        (chat ? "chat" : "responses") + (injected ? "-system" : "-baseline"),
      );
      assert.equal(result.code, 0, result.output);
      assert.ok(result.output.includes(reply), result.output);
      if (injected)
        assert.ok(JSON.stringify(requests.at(-1).body).includes(marker));
    }
    checks.push(
      (chat ? "Chat Completions" : "Responses") +
        ": JSON, SSE, count without inference, real isolated CLI baseline and system-message replay all pass",
    );
    const beforeInvalid = requests.length,
      invalid = await call("/v1/messages", {
        ...source,
        messages: [
          { role: "synthetic-secret-role", content: "synthetic-secret-text" },
        ],
      });
    assert.equal(invalid.status, 400);
    assert.ok(!(await invalid.text()).includes("synthetic-secret"));
    assert.equal(requests.length, beforeInvalid);
  }
  assert.deepEqual(pageErrors, []);
  const report = {
    root,
    legacy,
    checks,
    relayed,
    requests,
    pageErrors,
    limitation:
      "Synthetic localhost upstream only. Actual CLI requests are replayed with an injected documented system message; this does not claim the installed CLI emits system on its first print turn. Real user configurations and processes remain untouched.",
  };
  await writeFile(
    path.join(root, "report.json"),
    JSON.stringify(report, null, 2),
    "utf8",
  );
  await writeFile(
    path.resolve(
      ".qa/claude-system",
      legacy ? "before-report.json" : "after-report.json",
    ),
    JSON.stringify(report, null, 2),
    "utf8",
  );
  console.log(JSON.stringify({ root, checks, pageErrors }, null, 2));
} catch (error) {
  await writeFile(
    path.join(root, "failure.json"),
    JSON.stringify(
      { error: error.message, requests, relayed, checks, appOutput },
      null,
      2,
    ),
    "utf8",
  );
  throw error;
} finally {
  if (browser) await browser.close();
  if (app && app.exitCode === null)
    await new Promise((resolve) => {
      app.once("exit", resolve);
      app.kill();
    });
  for (const server of [previewServer?.httpServer, relay, upstream]) {
    if (!server) continue;
    server.closeAllConnections();
    await new Promise((resolve) => server.close(resolve));
  }
}
