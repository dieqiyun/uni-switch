import assert from "node:assert/strict";
import http from "node:http";
import path from "node:path";
import { spawn } from "node:child_process";
import { copyFile, mkdir, readFile, writeFile } from "node:fs/promises";
import { chromium } from "playwright";
import { preview } from "vite";

const root = path.resolve(".qa/claude-native-base", String(Date.now()));
const home = path.join(root, "home");
const local = path.join(root, "Local");
const cli = path.join(home, ".claude");
const codex = path.join(home, ".codex");
const roaming = path.join(root, "Roaming");
const temporary = path.join(root, "Temp");
await Promise.all(
  [cli, codex, local, roaming, temporary].map((directory) =>
    mkdir(directory, { recursive: true }),
  ),
);
const originalCodex = "model='isolated-original'\n";
const originalCli = { env: { KEEP: "synthetic-value" }, hooks: { Stop: [] } };
await writeFile(path.join(codex, "config.toml"), originalCodex, "utf8");
await writeFile(
  path.join(cli, "settings.json"),
  JSON.stringify(originalCli),
  "utf8",
);
const discoveryContext = path.join(root, "context.json");
await writeFile(
  discoveryContext,
  JSON.stringify({ home, local, roaming, roots: [home], notes: [], hints: [] }),
  "utf8",
);
const model = "claude-opus-5-5";
const models = [model, "claude-fable-5", "claude-haiku-5-5"];
const key = "synthetic-claude-native-base-key";
const requests = [];
const reply = "Native Claude path OK.";
const server = http.createServer(async (request, response) => {
  let text = "";
  for await (const chunk of request) text += chunk;
  const body = text ? JSON.parse(text) : {};
  const requestPath = new URL(request.url, "http://127.0.0.1").pathname;
  const accepted = request.headers.authorization === `Bearer ${key}`;
  const status = ![
    "/gateway/v1/messages",
    "/gateway/v1/messages/count_tokens",
    "/gateway/v1/models",
  ].includes(requestPath)
    ? 404
    : accepted
      ? 200
      : 401;
  requests.push({
    path: requestPath,
    model: body.model ?? null,
    stream: body.stream === true,
    authAccepted: accepted,
    status,
  });
  const send = (value) => {
    response.writeHead(status, { "content-type": "application/json" });
    response.end(JSON.stringify(value));
  };
  if (status !== 200)
    return send({
      type: "error",
      error: {
        type: status === 404 ? "invalid_request_error" : "authentication_error",
        message:
          status === 404 ? `Invalid URL (${request.url})` : "Invalid key",
      },
    });
  if (requestPath.endsWith("/models"))
    return send({
      data: models.map((id) => ({ id, type: "model" })),
      has_more: false,
      first_id: models[0],
      last_id: models.at(-1),
    });
  if (requestPath.endsWith("/count_tokens")) return send({ input_tokens: 10 });
  const message = {
    id: "msg_native_base_qa",
    type: "message",
    role: "assistant",
    model: body.model,
    content: [{ type: "text", text: reply }],
    stop_reason: "end_turn",
    stop_sequence: null,
    usage: { input_tokens: 10, output_tokens: 5 },
  };
  if (!body.stream) return send(message);
  response.writeHead(200, { "content-type": "text/event-stream" });
  for (const event of [
    {
      type: "message_start",
      message: {
        ...message,
        content: [],
        stop_reason: null,
        usage: { input_tokens: 10, output_tokens: 0 },
      },
    },
    {
      type: "content_block_start",
      index: 0,
      content_block: { type: "text", text: "" },
    },
    {
      type: "content_block_delta",
      index: 0,
      delta: { type: "text_delta", text: reply },
    },
    { type: "content_block_stop", index: 0 },
    {
      type: "message_delta",
      delta: { stop_reason: "end_turn", stop_sequence: null },
      usage: { output_tokens: 5 },
    },
    { type: "message_stop" },
  ])
    response.write(`event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`);
  response.end();
});
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const expectedBase = `http://127.0.0.1:${server.address().port}/gateway`;
const baseUrl = expectedBase + "/v1";
const checks = [];
const pageErrors = [];
let app, browser, page, previewServer;
let appOutput = "";

const portOpen = async () => {
  try {
    return (await fetch("http://127.0.0.1:9223/json/version")).ok;
  } catch {
    return false;
  }
};
const invoke = (command, args) =>
  page.evaluate(
    ({ command, args }) => window.__TAURI_INTERNALS__.invoke(command, args),
    { command, args },
  );

async function runClaude(settings, label) {
  const settingsPath = path.join(root, `${label}-settings.json`);
  await writeFile(settingsPath, JSON.stringify(settings), "utf8");
  const environment = { ...process.env };
  for (const name of Object.keys(environment))
    if (
      /^(ANTHROPIC_|CLAUDE_|AWS_|GOOGLE_|VERTEX_|FOUNDRY_|OPENAI_)/i.test(
        name,
      ) ||
      /^(https?|all|no)_proxy$/i.test(name)
    )
      delete environment[name];
  Object.assign(environment, {
    HOME: home,
    USERPROFILE: home,
    APPDATA: roaming,
    LOCALAPPDATA: local,
    TEMP: temporary,
    TMP: temporary,
    CLAUDE_CONFIG_DIR: path.join(root, `${label}-claude-config`),
    CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: "1",
    CLAUDE_CODE_SKIP_PROMPT_HISTORY: "1",
    DISABLE_TELEMETRY: "1",
    DISABLE_ERROR_REPORTING: "1",
    DISABLE_AUTOUPDATER: "1",
    NO_PROXY: "127.0.0.1,localhost",
    ...settings.env,
  });
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
      "Reply with one short greeting. Do not use tools.",
    ],
    { cwd: home, env: environment, windowsHide: true, stdio: "pipe" },
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
    "Existing native QA/CDP must remain untouched",
  );
  previewServer = await preview({
    root: path.resolve("."),
    preview: { host: "127.0.0.1", port: 1420, strictPort: true },
    clearScreen: false,
  });
  const executable = path.join(root, "uni-switch.exe");
  await copyFile(
    path.resolve("src-tauri/target/debug/uni-switch.exe"),
    executable,
  );
  app = spawn(executable, [], {
    windowsHide: true,
    stdio: "pipe",
    env: {
      ...process.env,
      HOME: home,
      APPDATA: roaming,
      LOCALAPPDATA: local,
      CODEX_HOME: codex,
      CLAUDE_CONFIG_DIR: cli,
      UNI_SWITCH_DATA_DIR: path.join(root, "data"),
      UNI_SWITCH_QA_DISCOVERY_CONTEXT: discoveryContext,
      WEBVIEW2_USER_DATA_FOLDER: path.join(root, "webview"),
      UNI_SWITCH_QA_UPDATE_REPOSITORY: "example/uni-switch",
      UNI_SWITCH_QA_UPDATE_ENDPOINT: expectedBase + "/github/releases/latest",
    },
  });
  app.stdin.end();
  app.stdout.on("data", (chunk) => (appOutput += chunk));
  app.stderr.on("data", (chunk) => (appOutput += chunk));
  const deadline = Date.now() + 25000;
  while (!(await portOpen())) {
    assert.ok(
      Date.now() < deadline,
      "Native QA startup timed out: " + appOutput,
    );
    assert.equal(app.exitCode, null, "Native QA exited: " + appOutput);
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  browser = await chromium.connectOverCDP("http://127.0.0.1:9223");
  page = browser.contexts()[0].pages()[0];
  page.on("pageerror", (error) => pageErrors.push(error.message));
  await page.getByRole("button", { name: "设置", exact: true }).waitFor();
  for (const [target, directory] of [
    ["claude_desktop", local],
    ["claude_cli", cli],
    ["codex", codex],
  ])
    await invoke("set_directory", { target, directory });
  const provider = await invoke("save_provider", {
    input: {
      id: null,
      family: "codex",
      name: "Native Claude URL QA",
      baseUrl,
      apiKey: key,
      model,
      authMode: "bearer",
      reasoningEffort: null,
      codexOptions: {
        upstreamProtocol: "anthropic",
        protocolDetectedAt: 1,
        models: models.map((id) => ({ id, enabled: true })),
      },
    },
  });
  for (const target of ["claude_desktop", "claude_cli"])
    assert.equal(
      (await invoke("apply_provider", { target, providerId: provider.id }))
        .state,
      "applied",
    );
  const settings = JSON.parse(
    await readFile(path.join(cli, "settings.json"), "utf8"),
  );
  const profile = JSON.parse(
    await readFile(
      path.join(
        local,
        "Claude-3p/configLibrary/e82de475-47fa-4c54-9000-13571c000001.json",
      ),
      "utf8",
    ),
  );
  assert.equal(settings.env.ANTHROPIC_BASE_URL, expectedBase);
  assert.equal(profile.inferenceGatewayBaseUrl, expectedBase);
  assert.equal(settings.env.ANTHROPIC_MODEL, model);
  assert.deepEqual(profile.inferenceModels, models);
  assert.equal(profile.inferenceGatewayAuthScheme, "bearer");
  assert.equal(profile.inferenceGatewayApiKey, key);
  const overview = await invoke("get_overview");
  assert.equal(
    overview.providers.find((entry) => entry.id === provider.id).baseUrl,
    baseUrl,
  );
  checks.push(
    "Both native Claude configurations strip only the terminal /v1; upstream, models and auth stay intact",
  );

  const body = {
    model,
    max_tokens: 16,
    messages: [{ role: "user", content: "hello" }],
  };
  const request = (base, suffix) =>
    fetch(base + suffix, {
      method: "POST",
      headers: {
        authorization: `Bearer ${key}`,
        "content-type": "application/json",
      },
      body: JSON.stringify(body),
    });
  assert.equal((await request(baseUrl, "/v1/messages")).status, 404);
  const desktopResponse = await request(
    profile.inferenceGatewayBaseUrl,
    "/v1/messages",
  );
  assert.equal(desktopResponse.status, 200);
  assert.equal((await desktopResponse.json()).model, model);
  assert.equal(
    (
      await request(
        settings.env.ANTHROPIC_BASE_URL,
        "/v1/messages/count_tokens",
      )
    ).status,
    200,
  );
  checks.push(
    "Old /v1/v1/messages reproduces 404; generated desktop Messages and CLI token-count paths return 200",
  );

  let cliVerification = "Skipped: UNI_SWITCH_CLAUDE_BIN is not configured";
  if (process.env.UNI_SWITCH_CLAUDE_BIN) {
    const beforeSettings = structuredClone(settings);
    beforeSettings.env.ANTHROPIC_BASE_URL = baseUrl;
    const beforeStart = requests.length;
    const before = await runClaude(beforeSettings, "before");
    assert.notEqual(before.code, 0, before.output);
    assert.ok(
      requests
        .slice(beforeStart)
        .some(
          (entry) =>
            entry.path === "/gateway/v1/v1/messages" && entry.status === 404,
        ),
      before.output,
    );
    const afterStart = requests.length;
    const after = await runClaude(settings, "after");
    assert.equal(after.code, 0, after.output);
    assert.ok(after.output.includes(reply), after.output);
    const afterRequests = requests
      .slice(afterStart)
      .filter((entry) => entry.path.endsWith("/messages"));
    assert.ok(afterRequests.length > 0);
    assert.ok(
      afterRequests.every(
        (entry) =>
          entry.path === "/gateway/v1/messages" &&
          entry.status === 200 &&
          entry.model === model &&
          entry.authAccepted,
      ),
    );
    cliVerification =
      "Actual isolated Claude Code reproduces 404 before and completes streamed inference after using the generated configuration";
    checks.push(cliVerification);
  }
  for (const target of ["claude_desktop", "claude_cli"])
    await invoke("restore_original", { target });
  assert.deepEqual(
    JSON.parse(await readFile(path.join(cli, "settings.json"), "utf8")),
    originalCli,
  );
  assert.equal(
    await readFile(path.join(codex, "config.toml"), "utf8"),
    originalCodex,
  );
  assert.deepEqual(pageErrors, []);
  checks.push(
    "Restore preserves original CLI values; Codex and the real clients remain untouched",
  );
  const result = {
    root,
    checks,
    cliVerification,
    requests,
    pageErrors,
    limitation:
      "Isolated uni-switch and optional isolated Claude Code CLI; desktop requests replay the generated profile. Synthetic localhost provider only, no real credentials, sessions or paid inference.",
  };
  for (const filename of [
    path.join(root, "results.json"),
    path.resolve(".qa/claude-native-base/results.json"),
  ])
    await writeFile(filename, JSON.stringify(result, null, 2), "utf8");
  console.log(JSON.stringify(result, null, 2));
} catch (error) {
  const failure = { root, checks, requests, pageErrors, error: error.message };
  await writeFile(
    path.join(root, "failure.json"),
    JSON.stringify(failure, null, 2),
    "utf8",
  );
  console.error(JSON.stringify(failure, null, 2));
  throw error;
} finally {
  if (browser) await browser.close();
  if (app && app.exitCode === null)
    await new Promise((resolve) => {
      app.once("exit", resolve);
      app.kill();
    });
  if (previewServer) {
    previewServer.httpServer.closeAllConnections();
    await new Promise((resolve) => previewServer.httpServer.close(resolve));
  }
  server.closeAllConnections();
  await new Promise((resolve) => server.close(resolve));
}
