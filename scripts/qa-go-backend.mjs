// Windows native IPC smoke for the embedded Go core. All state is isolated.
import assert from "node:assert/strict";
import http from "node:http";
import net from "node:net";
import { spawn } from "node:child_process";
import { createWriteStream } from "node:fs";
import { mkdir, readdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { chromium } from "playwright";
import AxeBuilder from "@axe-core/playwright";

if (process.platform !== "win32")
  throw new Error("Native Go core QA currently targets Windows");
const run = path.resolve(".qa/go-backend", Date.now().toString());
const dirs = Object.fromEntries(
  [
    "home",
    "data",
    "local",
    "roaming",
    "webview",
    "temp",
    "codex",
    "claude",
  ].map((name) => [name, path.join(run, name)]),
);
for (const directory of Object.values(dirs))
  await mkdir(directory, { recursive: true });
const sentinel = "# isolated Go backend QA; do not overwrite\n";
await writeFile(path.join(dirs.codex, "config.toml"), sentinel, "utf8");
const claudeSentinel =
  JSON.stringify({ env: { PERSONAL_QA: "keep" } }, null, 2) + "\n";
await writeFile(
  path.join(dirs.claude, "settings.json"),
  claudeSentinel,
  "utf8",
);
await mkdir(path.join(dirs.data, "model-metadata"), { recursive: true });
await writeFile(
  path.join(dirs.data, "model-metadata/models-dev.json"),
  JSON.stringify({
    openai: {
      models: {
        "gpt-4o": {
          id: "gpt-4o",
          limit: { context: 128000, output: 16384 },
          modalities: { input: ["text", "image"] },
        },
      },
    },
  }),
  "utf8",
);
const probe = net.createServer();
await new Promise((resolve, reject) => {
  probe.once("error", reject);
  probe.listen(9223, "127.0.0.1", resolve);
});
await new Promise((resolve) => probe.close(resolve));

const requests = [];
const inferences = [];
const key = "synthetic-native-go-key";
const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, "http://127.0.0.1");
  const authenticated =
    req.headers.authorization === `Bearer ${key}` ||
    req.headers["x-api-key"] === key ||
    req.headers["x-goog-api-key"] === key;
  requests.push({ path: url.pathname, authenticated });
  const send = (body, status = 200) => {
    res.writeHead(status, { "Content-Type": "application/json" });
    res.end(JSON.stringify(body));
  };
  if (!authenticated) return send({}, 401);
  if (req.method === "POST") {
    const chunks = [];
    for await (const chunk of req) chunks.push(chunk);
    const body = JSON.parse(Buffer.concat(chunks).toString("utf8"));
    inferences.push({ path: url.pathname, body });
    if (url.pathname === "/v1beta/models/gemini-native-test:generateContent")
      return send({
        candidates: [
          {
            content: { role: "model", parts: [{ text: "gemini-native-ok" }] },
            finishReason: "STOP",
          },
        ],
        usageMetadata: { promptTokenCount: 2, candidatesTokenCount: 1 },
      });
    if (url.pathname === "/v1beta/models/gemini-image-test:generateContent")
      return send({
        candidates: [
          {
            content: {
              parts: [
                { inlineData: { mimeType: "image/png", data: "c3ludGhldGlj" } },
              ],
            },
          },
        ],
      });
    if (url.pathname === "/media/v1/videos")
      return send({ id: "synthetic-remote-video", status: "queued" });
    if (url.pathname === "/vendor/check")
      return send({ native: true, custom: body.custom });
    if (url.pathname === "/native-anthropic/v1/messages") {
      if (body.stream) {
        res.writeHead(200, { "Content-Type": "text/event-stream" });
        const events = [
          { type: "message_start", message: { usage: { input_tokens: 3 } } },
          {
            type: "content_block_start",
            index: 0,
            content_block: { type: "text", text: "" },
          },
          {
            type: "content_block_delta",
            index: 0,
            delta: { type: "text_delta", text: "native-msg-ok" },
          },
          { type: "content_block_stop", index: 0 },
          {
            type: "message_delta",
            delta: { stop_reason: "end_turn" },
            usage: { output_tokens: 4 },
          },
          { type: "message_stop" },
        ];
        for (const event of events)
          res.write(`data: ${JSON.stringify(event)}\n\n`);
        return res.end();
      }
      return send({
        content: [{ type: "text", text: "native-msg-ok" }],
        stop_reason: "end_turn",
      });
    }
    if (url.pathname === "/site/v1/responses")
      return send({
        status: "completed",
        output: [
          {
            type: "message",
            content: [{ type: "output_text", text: "native-response-ok" }],
          },
        ],
      });
  }
  if (url.pathname === "/media/v1/videos/synthetic-remote-video")
    return send({
      id: "synthetic-remote-video",
      status: "completed",
      progress: 100,
    });
  if (url.pathname === "/media/v1/videos/synthetic-remote-video/content") {
    res.writeHead(200, { "Content-Type": "video/mp4" });
    return res.end("synthetic-mp4");
  }
  if (url.pathname === "/site/v1/models")
    return send({
      object: "list",
      data: [
        { id: "gpt-4o", modalities: { input: ["text"] } },
        { id: "private-native-test" },
      ],
    });
  if (url.pathname === "/site/v1/usage")
    return send({ mode: "unrestricted", balance: 0 });
  if (url.pathname === "/native-anthropic/models")
    return send({
      object: "list",
      data: [
        {
          id: "claude-native-test",
          supported_endpoint_types: ["anthropic", "openai"],
        },
      ],
    });
  if (url.pathname === "/denied/models")
    return send({ error: "do-not-echo-native-secret" }, 403);
  return send({}, 404);
});
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const origin = `http://127.0.0.1:${server.address().port}`;
let child, browser;
const waitUntil = async (fn, timeout = 60000) => {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    if (await fn()) return;
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error("Native Go QA timed out");
};
try {
  child = spawn(
    path.resolve(
      process.env.UNI_SWITCH_QA_EXE || "src-tauri/target/debug/uni-switch.exe",
    ),
    [],
    {
      windowsHide: true,
      stdio: ["ignore", "pipe", "pipe"],
      env: {
        ...process.env,
        UNI_SWITCH_DATA_DIR: dirs.data,
        WEBVIEW2_USER_DATA_FOLDER: dirs.webview,
        LOCALAPPDATA: dirs.local,
        APPDATA: dirs.roaming,
        CODEX_HOME: dirs.codex,
        CLAUDE_CONFIG_DIR: dirs.claude,
        TEMP: dirs.temp,
        TMP: dirs.temp,
        WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS:
          "--remote-debugging-port=9223 --disable-features=msWebOOUI,msPdfOOUI,msSmartScreenProtection",
        UNI_SWITCH_QA_EXIT_AFTER_MS: "90000",
      },
    },
  );
  const log = createWriteStream(path.join(run, "native.log"));
  child.stdout.pipe(log, { end: false });
  child.stderr.pipe(log, { end: false });
  child.once("exit", () => log.end());
  console.log("Native Go QA: waiting for isolated WebView");
  let spawnError;
  child.once("error", (error) => {
    spawnError = error;
  });
  await waitUntil(async () => {
    if (spawnError) throw spawnError;
    if (child.exitCode !== null)
      throw new Error(`QA app exited: ${child.exitCode}`);
    try {
      return (
        await fetch("http://127.0.0.1:9223/json/version", {
          signal: AbortSignal.timeout(1500),
        })
      ).ok;
    } catch {
      return false;
    }
  });
  browser = await chromium.connectOverCDP("http://127.0.0.1:9223");
  console.log("Native Go QA: checking desktop IPC");
  const page = browser.contexts()[0].pages()[0];
  await page.waitForFunction(() => Boolean(window.__TAURI_INTERNALS__?.invoke));
  const invoke = (name, args) =>
    page.evaluate(
      async ({ name, args }) => {
        try {
          return await window.__TAURI_INTERNALS__.invoke(name, args);
        } catch (error) {
          throw new Error(`${name}: ${JSON.stringify(error)}`);
        }
      },
      { name, args },
    );
  const input = { providerId: null, baseUrl: `${origin}/site/v1`, apiKey: key };
  const models = await invoke("discover_provider_connection", { input });
  assert.equal(models.protocol, "openai");
  assert.deepEqual(
    models.models.map((m) => m.id),
    ["gpt-4o", "private-native-test"],
  );
  assert.equal(models.models[0].metadata.capabilities.imageInput, false);
  assert.equal(models.models[0].metadata.catalog.inferredImages, true);
  assert.equal(models.models[0].metadata.profile.contextWindow, 128000);
  assert.equal(models.models[0].metadata.sources.imageInput, "vendor");
  assert.equal(models.models[0].contextWindow, null);
  assert.equal(models.models[1].metadata.profile.contextWindow, null);
  const draft = structuredClone(models.models[0]);
  draft.profileOverrides = { contextWindow: 1000000, maxOutputTokens: 2000000 };
  draft.capabilityOverrides = { imageInput: false };
  const draftBefore = JSON.stringify(draft);
  const resolved = await invoke("resolve_model_profiles", {
    baseUrl: input.baseUrl,
    models: [draft],
    providerContext: 256000,
  });
  assert.equal(resolved[0].metadata.profile.contextWindow, 1000000);
  assert.equal(resolved[0].metadata.publishedOutput, 1000000);
  assert.equal(resolved[0].metadata.sources.imageInput, "user:model");
  assert.equal(
    JSON.stringify(draft),
    draftBefore,
    "resolving a draft must not mutate the caller",
  );
  assert.deepEqual(
    (await invoke("get_overview")).providers,
    [],
    "previewing metadata must not save a provider",
  );
  const synced = await invoke("sync_provider_models", {
    input,
    authMode: "bearer",
    protocol: "openai",
  });
  assert.equal(synced.models.length, 2);
  const balances = await Promise.all(
    Array.from({ length: 4 }, () =>
      invoke("query_provider_balance", { input }),
    ),
  );
  assert.ok(balances.every((r) => r.amount === 0 && r.scope === "账户余额"));
  const saved = await invoke("save_provider", {
    input: {
      id: null,
      family: "codex",
      name: "Isolated Go protocol detection",
      baseUrl: `${origin}/native-anthropic`,
      apiKey: key,
      model: "claude-native-test",
      authMode: "bearer",
      reasoningEffort: null,
      codexOptions: {
        upstreamProtocol: "anthropic",
        models: [
          {
            id: "claude-native-test",
            enabled: true,
            contextWindow: 128000,
            reasoningEfforts: [],
            profile: { midConversationSystem: true },
          },
        ],
      },
    },
  });
  const detected = await invoke("detect_provider_protocol", {
    expected: saved,
  });
  assert.equal(detected.codexOptions.upstreamProtocol, "anthropic");
  assert.ok(detected.codexOptions.models.length > 0);
  assert.ok(
    detected.codexOptions.models.every(
      (model) => !("midConversationSystem" in (model.profile || {})),
    ),
  );
  const probeArgs = {
    input: { providerId: saved.id, baseUrl: saved.baseUrl, apiKey: null },
    model: "claude-native-test",
    authMode: "bearer",
    endpoint: "messages",
  };
  await assert.rejects(
    invoke("verify_model_connection", { ...probeArgs, consent: false }),
  );
  assert.equal(
    inferences.length,
    0,
    "no request without explicit test consent",
  );
  const probeResult = await invoke("verify_model_connection", {
    ...probeArgs,
    consent: true,
  });
  assert.equal(probeResult.state, "available");
  assert.equal(inferences.length, 1);
  assert.equal(inferences[0].body.max_tokens, 16);
  assert.deepEqual(inferences[0].body.messages, [
    { role: "user", content: "hi" },
  ]);
  await assert.rejects(readdir(path.join(dirs.data, "model-verifications")), {
    code: "ENOENT",
  });
  await invoke("set_directory", { target: "codex", directory: dirs.codex });
  await invoke("set_directory", {
    target: "claude_cli",
    directory: dirs.claude,
  });
  const applied = await invoke("apply_provider", {
    target: "codex",
    providerId: saved.id,
  });
  assert.equal(applied.state, "applied");
  const route = JSON.parse(
    await readFile(path.join(dirs.data, "protocol-bridge.json"), "utf8"),
  );
  const bridgeOrigin = `http://127.0.0.1:${route.port}`;
  const goHealth = await fetch(`${bridgeOrigin}/health`, {
    headers: { Authorization: `Bearer ${route.token}` },
    signal: AbortSignal.timeout(5000),
  });
  assert.equal(goHealth.status, 204);
  assert.equal(goHealth.headers.get("x-uni-switch-core"), "go/1");
  const requestBridge = async (pathname, body, headers = {}) => {
    const response = await fetch(bridgeOrigin + pathname, {
      method: body ? "POST" : "GET",
      headers: {
        Authorization: `Bearer ${route.token}`,
        "Content-Type": "application/json",
        ...headers,
      },
      body: body ? JSON.stringify(body) : undefined,
      signal: AbortSignal.timeout(10000),
    });
    return { status: response.status, text: await response.text() };
  };
  const responsesPath = `/v1/${saved.id}/responses`;
  const responseBody = { model: "claude-native-test", input: "hi" };
  let forwarded = await requestBridge(responsesPath, responseBody);
  assert.equal(forwarded.status, 200, forwarded.text);
  assert.equal(
    JSON.parse(forwarded.text).output[0].content[0].text,
    "native-msg-ok",
  );
  const streamed = await requestBridge(responsesPath, {
    ...responseBody,
    stream: true,
  });
  assert.equal(streamed.status, 200, streamed.text);
  assert.ok(
    streamed.text.includes("response.completed") &&
      streamed.text.includes("native-msg-ok"),
  );
  const catalog = await requestBridge(`/v1/${saved.id}/models`);
  assert.equal(catalog.status, 200, catalog.text);
  assert.ok(
    JSON.parse(catalog.text).data.some(
      (model) => model.id === "claude-native-test",
    ),
  );
  assert.equal(
    (
      await requestBridge(responsesPath, responseBody, {
        Origin: "https://synthetic.test",
      })
    ).status,
    401,
  );
  assert.equal(
    (
      await requestBridge(responsesPath, responseBody, {
        Authorization: "Bearer invalid",
      })
    ).status,
    401,
  );
  await invoke("save_provider", {
    input: {
      ...detected,
      apiKey: "synthetic-unapplied-native-key",
    },
  });
  forwarded = await requestBridge(responsesPath, responseBody);
  assert.equal(forwarded.status, 200, forwarded.text);
  assert.equal(
    JSON.parse(forwarded.text).output[0].content[0].text,
    "native-msg-ok",
  );
  const openai = await invoke("save_provider", {
    input: {
      id: null,
      family: "codex",
      name: "Isolated native reverse gateway",
      baseUrl: input.baseUrl,
      apiKey: key,
      model: "gpt-4o",
      authMode: "bearer",
      reasoningEffort: null,
      codexOptions: { upstreamProtocol: "openai", models: [models.models[0]] },
    },
  });
  const reverseApplied = await invoke("apply_provider", {
    target: "claude_cli",
    providerId: openai.id,
  });
  assert.equal(reverseApplied.state, "applied");
  const messagesPath = `/claude/claude_cli/${openai.id}/v1/messages`;
  const reverse = await requestBridge(messagesPath, {
    model: "gpt-4o",
    messages: [{ role: "user", content: "hi" }],
  });
  assert.equal(reverse.status, 200, reverse.text);
  assert.equal(JSON.parse(reverse.text).content[0].text, "native-response-ok");
  await invoke("restore_original", { target: "codex" });
  assert.equal((await requestBridge(responsesPath, responseBody)).status, 409);
  assert.equal(
    (
      await requestBridge(messagesPath, {
        model: "gpt-4o",
        messages: [{ role: "user", content: "hi" }],
      })
    ).status,
    200,
  );
  await invoke("restore_original", { target: "claude_cli" });
  assert.equal(
    (
      await requestBridge(messagesPath, {
        model: "gpt-4o",
        messages: [{ role: "user", content: "hi" }],
      })
    ).status,
    409,
  );
  assert.equal(
    await readFile(path.join(dirs.claude, "settings.json"), "utf8"),
    claudeSentinel,
  );
  await invoke("save_provider", { input: { ...detected, apiKey: key } });
  // New platform UI/IPC acceptance uses only the localhost fixture above.
  const platform = (action, params = {}) =>
    invoke("platform_command", { action, params });
  assert.deepEqual(await platform("accounts"), []);
  await page.getByRole("button", { name: "设置", exact: true }).click();
  const panel = page.locator(".platform-panel");
  await panel.getByText(/网关来源（/).click();
  await panel
    .getByRole("button", { name: "添加网关来源", exact: true })
    .click();
  await panel.getByLabel("来源名称", { exact: true }).fill("Native Gemini QA");
  await panel.getByLabel("API 地址", { exact: true }).fill(`${origin}/v1beta`);
  await panel.getByLabel("API Key", { exact: true }).fill(key);
  await panel.getByLabel("上游协议", { exact: true }).selectOption("gemini");
  await panel
    .getByLabel("模型列表", { exact: true })
    .fill("chat gemini-native-test\nimage gemini-image-test");
  const sourceEditorAxe = await new AxeBuilder({ page })
    .include(".platform-panel")
    .analyze();
  assert.deepEqual(sourceEditorAxe.violations, []);
  await page.screenshot({ path: path.join(run, "platform-source-editor.png") });
  await panel.getByRole("button", { name: "保存来源", exact: true }).click();
  await panel.getByText("网关来源已保存。").waitFor();
  const sources = await platform("sources");
  const geminiSource = sources.find((s) => s.name === "Native Gemini QA");
  assert.ok(geminiSource && !JSON.stringify(geminiSource).includes(key));
  const unified = await platform("info");
  const unifiedRequest = async (resource, body, method = "POST") => {
    const result = await fetch(`http://127.0.0.1:${unified.port}${resource}`, {
      method,
      headers: {
        Authorization: `Bearer ${unified.token}`,
        "Content-Type": "application/json",
      },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: AbortSignal.timeout(5000),
    });
    return { status: result.status, text: await result.text() };
  };
  const scopedModel = `${geminiSource.id}/gemini-native-test`;
  const unifiedReply = await unifiedRequest("/v1/responses", {
    model: scopedModel,
    input: "hello",
  });
  assert.equal(unifiedReply.status, 200, unifiedReply.text);
  assert.ok(unifiedReply.text.includes("gemini-native-ok"));
  const imageReply = await unifiedRequest("/v1/images/generations", {
    model: `${geminiSource.id}/gemini-image-test`,
    prompt: "synthetic image",
  });
  assert.equal(imageReply.status, 200, imageReply.text);
  assert.equal(JSON.parse(imageReply.text).data[0].b64_json, "c3ludGhldGlj");
  const nativeReply = await unifiedRequest(
    `/v1/native/${geminiSource.id}/vendor/check`,
    { custom: "preserved" },
  );
  assert.equal(nativeReply.status, 200, nativeReply.text);
  assert.equal(JSON.parse(nativeReply.text).custom, "preserved");
  const videoSource = await platform("save", {
    profile: {
      id: "",
      name: "Video QA",
      baseUrl: `${origin}/media/v1`,
      protocol: "responses",
      authMode: "bearer",
      apiKey: key,
      enabled: true,
      vendor: "openai",
      models: [{ id: "sora-test", kind: "video" }],
    },
  });
  const videoReply = await unifiedRequest("/v1/videos", {
    model: `${videoSource}/sora-test`,
    prompt: "synthetic video",
  });
  assert.equal(videoReply.status, 200, videoReply.text);
  const videoID = JSON.parse(videoReply.text).id;
  const statusReply = await unifiedRequest(
    `/v1/videos/${videoID}`,
    undefined,
    "GET",
  );
  assert.equal(JSON.parse(statusReply.text).status, "completed");
  const contentReply = await unifiedRequest(
    `/v1/videos/${videoID}/content`,
    undefined,
    "GET",
  );
  assert.equal(contentReply.text, "synthetic-mp4");
  const sourceRow = panel
    .getByText("Native Gemini QA", { exact: true })
    .locator("..");
  await sourceRow
    .getByRole("button", { name: "添加到供应商列表", exact: true })
    .click();
  await panel.getByText("已添加到供应商列表，可选择客户端并应用。").waitFor();
  assert.ok(
    (await invoke("get_overview")).providers.some(
      (p) => p.model === scopedModel,
    ),
  );
  // Generated loopback providers must never be imported as their own upstream.
  assert.ok(
    !(await platform("sources")).some((p) => p.name.endsWith("· 统一网关")),
  );
  await panel.getByRole("button", { name: "显示本地网关连接" }).click();
  await panel.getByLabel("本地网关 Key").waitFor();
  assert.equal(
    await panel.getByLabel("本地网关 Key").getAttribute("type"),
    "password",
  );
  await panel.getByText(/订阅账号管理（/).click();
  const panelAxe = await new AxeBuilder({ page })
    .include(".platform-panel")
    .analyze();
  assert.deepEqual(panelAxe.violations, []);
  await panel
    .getByRole("heading", { name: "订阅账号与统一网关" })
    .scrollIntoViewIfNeeded();
  const fit = await page
    .locator('[role="dialog"]')
    .evaluate((el) => ({ width: el.clientWidth, scroll: el.scrollWidth }));
  assert.ok(fit.scroll <= fit.width + 1, JSON.stringify(fit));
  await page.screenshot({ path: path.join(run, "platform-settings.png") });
  await page.getByRole("button", { name: "关闭弹窗" }).click();
  // Earlier isolated configuration writes queue one notice per primary client.
  // Close each notice without invoking any restart action.
  for (let notice = 0; notice < 6; notice++) {
    const acknowledge = page.getByRole("button", {
      name: /^(稍后重启|知道了，去新开对话)$/,
    });
    if (!(await acknowledge.isVisible())) break;
    await acknowledge.click();
    await page.waitForTimeout(100);
  }
  const additionalClients = [
    ["opencode", "chat_completions"],
    ["pi", "chat_completions"],
    ["gemini", "gemini"],
    ["t3code", "messages"],
    ["muse", "responses"],
    ["fx", "chat_completions"],
    ["claudescience", "messages"],
    ["cursorlocal", "chat_completions"],
    ["openchamber", "chat_completions"],
    ["cindy", "messages"],
    ["lody", "messages"],
  ];
  for (const [client, protocol] of additionalClients) {
    await invoke("set_client_config_directory", {
      client,
      directory: path.join(run, "clients", client),
    });
    const written = await invoke("apply_client_config", {
      client,
      providerId: saved.id,
      protocol,
      expectedRevision: null,
    });
    const state = await invoke("get_client_config_status", { client });
    assert.equal(state.activeProviderId, saved.id, client);
    if (client === "cindy" || client === "lody") {
      assert.ok(written.importLink.startsWith(`${client}://`));
      assert.equal(state.state, "awaiting_import");
    } else if (
      ["claudescience", "cursorlocal", "openchamber"].includes(client)
    ) {
      assert.equal(state.state, "launch_ready");
    }
    if (client === "cursorlocal") {
      const response = await requestBridge(
        `/clients/cursorlocal/${saved.id}/v1/models`,
      );
      assert.equal(response.status, 200, response.text);
      const model = JSON.parse(response.text).data.find(
        (m) => m.id === "claude-native-test",
      );
      assert.deepEqual(model.api_types, ["anthropic_messages"]);
      assert.equal(model.capabilities.supports_streaming, true);
      assert.equal(model.capabilities.supports_tool_use, true);
    }
    if (client === "pi") {
      const response = await requestBridge(
        `/clients/pi/${saved.id}/v1/chat/completions`,
        {
          model: "claude-native-test",
          messages: [{ role: "user", content: "hi" }],
        },
      );
      assert.equal(response.status, 200, response.text);
      assert.equal(
        JSON.parse(response.text).choices[0].message.content,
        "native-msg-ok",
      );
    }
    if (client === "gemini") {
      const response = await requestBridge(
        `/clients/gemini/${saved.id}/v1beta/models/claude-native-test:generateContent`,
        { contents: [{ role: "user", parts: [{ text: "hi" }] }] },
      );
      assert.equal(response.status, 200, response.text);
      assert.equal(
        JSON.parse(response.text).candidates[0].content.parts[0].text,
        "native-msg-ok",
      );
    }
    if (client === "muse" || client === "fx") {
      const scoped = JSON.parse(
        await readFile(
          path.join(dirs.data, "client-endpoints", `${client}.json`),
          "utf8",
        ),
      );
      const response = await fetch(
        `http://127.0.0.1:${scoped.port}/v1/models`,
        { signal: AbortSignal.timeout(5000) },
      );
      assert.equal(response.status, 200);
      assert.ok(
        (await response.json()).data.some((m) => m.id === "claude-native-test"),
      );
    }
    await invoke("restore_client_config", { client });
    assert.equal(
      (await invoke("get_client_config_status", { client })).activeProviderId,
      null,
    );
  }
  await page.getByRole("button", { name: /全部客户端/ }).click();
  await page.getByRole("searchbox", { name: "搜索客户端" }).fill("t3");
  await page.locator('[role="dialog"]').evaluate(async (element) => {
    await Promise.all(element.getAnimations({ subtree: true }).map((animation) => animation.finished.catch(() => {})));
  });
  const accessibility = await new AxeBuilder({ page })
    .include('[role="dialog"]')
    .analyze();
  assert.deepEqual(accessibility.violations, []);
  await page.screenshot({ path: path.join(run, "client-chooser.png") });
  await page.getByRole("button", { name: "T3 Code", exact: true }).click();
  await page.getByRole("heading", { name: "一键配置 T3 Code" }).waitFor();
  const denied = await page.evaluate(
    async (input) => {
      try {
        await window.__TAURI_INTERNALS__.invoke(
          "discover_provider_connection",
          { input },
        );
        return null;
      } catch (error) {
        return { code: error.code, message: error.message };
      }
    },
    { ...input, baseUrl: `${origin}/denied` },
  );
  assert.equal(denied?.code, "supplier_http");
  assert.ok(
    !JSON.stringify(denied).includes("do-not-echo-native-secret") &&
      !JSON.stringify(denied).includes(key),
  );
  const payloads = (await readdir(dirs.temp, { withFileTypes: true })).filter(
    (e) => e.isDirectory() && e.name.startsWith("uni-switch-core-"),
  );
  assert.equal(
    payloads.length,
    1,
    "native binary must release exactly its own embedded core payload",
  );
  const payload = path.join(dirs.temp, payloads[0].name, "uni-switch-core.exe");
  assert.ok((await readFile(payload)).subarray(0, 2).equals(Buffer.from("MZ")));
  const metadataPayloads = (
    await readdir(dirs.temp, { withFileTypes: true })
  ).filter((e) => e.isDirectory() && e.name.startsWith("uni-switch-metadata-"));
  assert.equal(metadataPayloads.length, 1);
  const metadataPayload = path.join(
    dirs.temp,
    metadataPayloads[0].name,
    "core.exe",
  );
  assert.ok(
    (await readFile(metadataPayload)).subarray(0, 2).equals(Buffer.from("MZ")),
  );
  assert.equal(
    await readFile(path.join(dirs.codex, "config.toml"), "utf8"),
    sentinel,
  );
  console.log("Native Go QA: functional checks passed; waiting for owned exit");
  await waitUntil(() => child.exitCode !== null, 95000);
  assert.equal(
    child.exitCode,
    0,
    "our QA app must finish its normal exit path",
  );
  await assert.rejects(readFile(payload), { code: "ENOENT" });
  await assert.rejects(readFile(metadataPayload), { code: "ENOENT" });
  const result = {
    passed: true,
    commands: [
      "discover_provider_connection",
      "sync_provider_models",
      "detect_provider_protocol",
      "query_provider_balance",
      "resolve_model_profiles",
      "save_provider",
      "apply_provider",
      "restore_original",
      "verify_model_connection",
    ],
    embeddedCore: true,
    concurrentQueries: 4,
    unknownAndExplicitFalseMetadata: true,
    originalConfigurationUnchanged: true,
    normalExitCleansEmbeddedCore: true,
    normalExitCleansMetadataCore: true,
    draftResolutionDoesNotSaveProvider: true,
    catalogVendorAndUserPrecedence: true,
    nativeGatewayBothDirectionsWithModels: true,
    nativeGatewaySSE: true,
    nativeGatewayAuthorization: true,
    goOwnsLocalListener: true,
    unappliedDraftDoesNotChangeRoute: true,
    restoreDisablesOnlyItsTarget: true,
    isolatedClientFilesRestored: true,
    single16TokenConnectivityProbeWithConsent: true,
    noLegacyCapabilityObservationFiles: true,
    retiredInlineSystemReadAndDiscarded: true,
    additionalClients: additionalClients.map(([id]) => id),
    geminiAndPiNativeIPCForwarding: true,
    importAndLaunchStatesAccurate: true,
    clientChooserAccessible: true,
    unifiedGeminiNativeIPC: true,
    platformSourcesUIAndSupplierRegistration: true,
    platformSourcesNoCredentialExposure: true,
    platformImageVideoAndNativeRoutes: true,
    platformAxeNoViolations: true,
    requests,
  };
  await writeFile(
    path.join(run, "results.json"),
    JSON.stringify(result, null, 2) + "\n",
    "utf8",
  );
  console.log(JSON.stringify({ ...result, runDirectory: run }));
} catch (error) {
  const failedPage = browser?.contexts()[0]?.pages()[0];
  if (failedPage && !failedPage.isClosed()) {
    await failedPage.screenshot({ path: path.join(run, "failure.png") }).catch(() => {});
    await writeFile(path.join(run, "failure-ui.txt"), await failedPage.locator("body").innerText().catch(() => "Page unavailable"), "utf8");
  }
  throw error;
} finally {
  if (browser) await browser.close();
  if (child && child.exitCode === null)
    await new Promise((resolve) => {
      child.once("exit", resolve);
      child.kill();
    });
  server.closeAllConnections();
  await new Promise((resolve) => server.close(resolve));
}
