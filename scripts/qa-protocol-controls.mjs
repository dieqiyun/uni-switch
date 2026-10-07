import assert from "node:assert/strict";
import http from "node:http";
import path from "node:path";
import { spawn } from "node:child_process";
import { copyFile, mkdir, readFile, writeFile } from "node:fs/promises";
import { chromium } from "playwright";
import AxeBuilder from "@axe-core/playwright";
import { verifyCenteredPromotion } from "./qa-brand-refresh.mjs";

const root = path.resolve(".qa/protocol-controls", String(Date.now()));
const home = path.join(root, "用户 中文");
const codex = path.join(home, ".codex");
const cli = path.join(home, ".claude");
const local = path.join(root, "Local");
const dataDirectory = path.join(root, "data");
await Promise.all(
  [codex, cli, local, "docs/screenshots"].map((p) =>
    mkdir(p, { recursive: true }),
  ),
);
const originalCodex = "# isolated original\nmodel='original'\n";
const originalCli = {
  env: { CUSTOM_QA: "preserved", ANTHROPIC_MODEL: "original" },
};
await writeFile(path.join(codex, "config.toml"), originalCodex);
await writeFile(path.join(cli, "settings.json"), JSON.stringify(originalCli));
const contextFile = path.join(root, "context.json");
await writeFile(
  contextFile,
  JSON.stringify({
    home,
    local,
    roaming: null,
    roots: [home],
    notes: [],
    hints: [],
  }),
);
const key = "isolated-protocol-controls-key";
let failModels = true;
const modelRequests = [],
  inferenceRequests = [],
  checks = [],
  audits = [],
  errors = [];
const server = http.createServer(async (req, res) => {
  const send = (value, code = 200) => {
    res.writeHead(code, { "Content-Type": "application/json" });
    res.end(JSON.stringify(value));
  };
  if (req.url === "/github/releases/latest") return send({}, 404);
  const anthropic = req.url.startsWith("/anthropic/");
  if (req.url.endsWith("/models")) {
    modelRequests.push({
      path: req.url,
      bearer: req.headers.authorization === `Bearer ${key}`,
      apiKey: req.headers["x-api-key"] === key,
    });
    if (req.url.startsWith("/retry/") && failModels) return send({}, 503);
    if (anthropic && req.headers["x-api-key"] !== key) return send({}, 401);
    if (!anthropic && req.headers.authorization !== `Bearer ${key}`)
      return send({}, 401);
    return send(
      anthropic
        ? {
            data: [{ id: "claude-sonnet-4-6", type: "model" }],
            has_more: false,
          }
        : {
            object: "list",
            data: [
              { id: "claude-sonnet-4-6", object: "model" },
              { id: "gpt-5.4", object: "model" },
            ],
          },
    );
  }
  if (req.url.endsWith("/usage"))
    return send({ balance: 5.8, unit: "USD", mode: "unrestricted" });
  if (req.method === "POST") {
    let raw = "";
    for await (const chunk of req) raw += chunk;
    const body = JSON.parse(raw);
    inferenceRequests.push({ path: req.url, model: body.model });
    if (req.url.endsWith("/messages"))
      return send({
        id: "msg_qa",
        type: "message",
        role: "assistant",
        model: body.model,
        content: [{ type: "text", text: "Claude converted reply" }],
        stop_reason: "end_turn",
        usage: { input_tokens: 4, output_tokens: 3 },
      });
    if (req.url.endsWith("/responses"))
      return send({
        id: "resp_qa",
        object: "response",
        status: "completed",
        model: body.model,
        output: [
          {
            id: "msg_qa",
            type: "message",
            role: "assistant",
            status: "completed",
            content: [
              {
                type: "output_text",
                text: "OpenAI converted reply",
                annotations: [],
              },
            ],
          },
        ],
        usage: { input_tokens: 4, output_tokens: 3 },
      });
  }
  send({}, 404);
});
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const origin = `http://127.0.0.1:${server.address().port}`;
let app, browser, page;
async function until(fn) {
  const end = Date.now() + 20000;
  while (Date.now() < end) {
    if (await fn()) return;
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error("Protocol controls QA timeout");
}
const portOpen = async () => {
  try {
    return (await fetch("http://127.0.0.1:9223/json/version")).ok;
  } catch {
    return false;
  }
};
async function invoke(name, args = {}) {
  const result = await page.evaluate(
    async ({ name, args }) => {
      try {
        return { value: await window.__TAURI_INTERNALS__.invoke(name, args) };
      } catch (error) {
        return { error };
      }
    },
    { name, args },
  );
  if (result.error)
    throw Object.assign(new Error(result.error.message), result.error);
  return result.value;
}
const overview = () => invoke("get_overview");
const saved = async (id) =>
  (await overview()).providers.find((p) => p.id === id);
const status = async (target) =>
  (await overview()).targets.find((s) => s.target === target);
const row = (name) =>
  page.locator(".provider-card").filter({
    has: page.getByRole("button", {
      name: `修改名称 ${name}`,
      exact: true,
      includeHidden: true,
    }),
  });
async function files(target) {
  return Promise.all(
    (await status(target)).files.map(async (file) => [
      file,
      await readFile(file, "utf8").catch((e) => {
        if (e.code === "ENOENT") return null;
        throw e;
      }),
    ]),
  );
}
async function audit(label) {
  const result = await new AxeBuilder({ page }).analyze();
  const violations = result.violations.map((v) => ({
    id: v.id,
    targets: v.nodes.map((n) => n.target),
  }));
  audits.push({ label, violations });
  assert.deepEqual(violations, []);
}
async function changeConversion(name, enabled) {
  const label = row(name).locator(".provider-conversion-toggle");
  const control = label.getByRole("switch", { includeHidden: true });
  assert.equal(await control.isChecked(), !enabled);
  await label.click();
  await until(
    async () =>
      (await control.isChecked()) === enabled && (await control.isEnabled()),
  );
}
async function dismissRestart() {
  const dialog = page.getByRole("dialog", {
    name: "重启 Codex 使配置生效",
    exact: true,
  });
  await dialog.waitFor();
  await dialog.getByRole("button", { name: "稍后重启", exact: true }).click();
}
async function refresh() {
  await page.getByRole("button", { name: "刷新配置状态", exact: true }).click();
}
async function selectClaude(target) {
  await page
    .getByRole("tab", { name: "Claude Code 桌面端与 CLI", exact: true })
    .click();
  await page
    .getByRole("button", {
      name: target === "claude_cli" ? "CLI" : "桌面端",
      exact: true,
    })
    .click();
}
try {
  assert.equal(await portOpen(), false, "QA debug port is unused");
  const executable = path.join(root, "uni-switch.exe");
  await copyFile(
    path.resolve("src-tauri/target/debug/uni-switch.exe"),
    executable,
  );
  app = spawn(executable, [], {
    windowsHide: true,
    stdio: "ignore",
    env: {
      ...process.env,
      UNI_SWITCH_DATA_DIR: dataDirectory,
      UNI_SWITCH_QA_DISCOVERY_CONTEXT: contextFile,
      WEBVIEW2_USER_DATA_FOLDER: path.join(root, "webview"),
      CODEX_HOME: codex,
      CLAUDE_CONFIG_DIR: cli,
      LOCALAPPDATA: local,
      UNI_SWITCH_QA_UPDATE_REPOSITORY: "example/uni-switch",
      UNI_SWITCH_QA_UPDATE_ENDPOINT: `${origin}/github/releases/latest`,
    },
  });
  await until(portOpen);
  browser = await chromium.connectOverCDP("http://127.0.0.1:9223");
  page = browser.contexts()[0].pages()[0];
  page.setDefaultTimeout(15000);
  page.on("pageerror", (e) => errors.push(e.message));
  await page.getByRole("button", { name: "设置", exact: true }).waitFor();
  for (const [target, directory] of [
    ["codex", codex],
    ["claude_cli", cli],
    ["claude_desktop", local],
  ])
    await invoke("set_directory", { target, directory });
  const desktopOriginals = await files("claude_desktop");
  async function add(name, route, family, model) {
    return invoke("save_provider", {
      input: {
        id: null,
        name,
        family,
        baseUrl: `${origin}/${route}/v1`,
        apiKey: key,
        authMode: "bearer",
        model,
        reasoningEffort: null,
        codexOptions: {
          models: [
            {
              id: model,
              contextWindow: 256000,
              reasoningEfforts: [],
              enabled: true,
            },
          ],
        },
      },
    });
  }
  const openai = await add(
    "OpenAI 兼容供应商",
    "openai",
    "codex",
    "claude-sonnet-4-6",
  );
  const claude = await add(
    "Claude 原生供应商",
    "anthropic",
    "claude",
    "claude-sonnet-4-6",
  );
  const retry = await add("待重试供应商", "retry", "codex", "gpt-5.4");
  await refresh();
  await until(
    async () =>
      !!(await saved(openai.id)).codexOptions.protocolDetectedAt &&
      !!(await saved(claude.id)).codexOptions.protocolDetectedAt,
  );
  await row(retry.name).getByText("协议检测失败", { exact: true }).waitFor();
  assert.ok(
    await row(retry.name)
      .getByRole("button", { name: "使用", exact: true })
      .isDisabled(),
  );
  assert.equal(
    await row(retry.name)
      .getByRole("switch", { name: /转换为/ })
      .count(),
    0,
  );
  failModels = false;
  await row(retry.name)
    .getByRole("button", { name: "重新检测", exact: true })
    .click();
  await until(
    async () => !!(await saved(retry.id)).codexOptions.protocolDetectedAt,
  );
  assert.equal(
    await row(openai.name).locator(".provider-protocol-value").textContent(),
    "OpenAI自动检测",
  );
  assert.equal(
    await row(openai.name)
      .getByRole("switch", { name: /转换为/ })
      .count(),
    0,
  );
  assert.equal(
    await row(claude.name).locator(".provider-protocol-value").textContent(),
    "Claude自动检测",
  );
  assert.ok(
    await row(claude.name)
      .getByRole("switch", { name: "转换为 OpenAI · Claude 原生供应商" })
      .isChecked(),
  );
  assert.equal((await saved(claude.id)).authMode, "x-api-key");
  assert.equal(
    await readFile(path.join(codex, "config.toml"), "utf8"),
    originalCodex,
  );
  assert.deepEqual(
    (await saved(openai.id)).codexOptions.models.map((m) => m.id),
    ["claude-sonnet-4-6"],
  );
  checks.push(
    "旧配置自动识别接口协议和认证，Claude模型经OpenAI接口仍显示OpenAI；失败可重试，检测不写客户端或覆盖模型选择",
  );
  await invoke("delete_provider", { providerId: retry.id });
  await refresh();
  await row(retry.name).waitFor({ state: "hidden" });
  await page.setViewportSize({ width: 1120, height: 780 });
  await audit("Codex protocol list");
  await verifyCenteredPromotion(page);
  await page.screenshot({
    path: "docs/screenshots/protocol-controls-codex-local.png",
  });

  await changeConversion(claude.name, false);
  await assert.rejects(
    invoke("apply_provider", { target: "codex", providerId: claude.id }),
    (e) => e.code === "conversion_required",
  );
  assert.equal(
    await readFile(path.join(codex, "config.toml"), "utf8"),
    originalCodex,
  );
  assert.ok(
    await row(claude.name)
      .getByRole("button", { name: "使用", exact: true })
      .isDisabled(),
  );
  await changeConversion(claude.name, true);
  assert.equal((await status("codex")).activeProviderId, null);
  await row(claude.name)
    .getByRole("button", { name: "使用", exact: true })
    .click();
  await dismissRestart();
  const config = await readFile(path.join(codex, "config.toml"), "utf8");
  const bridge = JSON.parse(
    await readFile(path.join(dataDirectory, "protocol-bridge.json"), "utf8"),
  );
  const base = `http://127.0.0.1:${bridge.port}/v1/${claude.id}`;
  assert.ok(config.includes(base));
  const response = await fetch(`${base}/responses`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${bridge.token}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      model: "claude-sonnet-4-6",
      input: "Hello",
      stream: false,
    }),
  });
  assert.equal(response.status, 200);
  assert.ok(
    JSON.stringify(await response.json()).includes("Claude converted reply"),
  );
  checks.push(
    "Codex转换默认开启；关闭拒绝应用，开启不自动切换；使用后实际Responses转Messages请求成功并提示重启",
  );
  await invoke("apply_provider", {
    target: "claude_cli",
    providerId: claude.id,
  });
  const cliBefore = await files("claude_cli");
  const revision = (await status("codex")).configurationRevision;
  await changeConversion(claude.name, false);
  await dismissRestart();
  assert.equal((await status("codex")).activeProviderId, null);
  assert.ok((await status("codex")).configurationRevision > revision);
  assert.ok(
    (await readFile(path.join(codex, "config.toml"), "utf8")).includes(
      "original",
    ),
  );
  assert.ok(
    !(await readFile(path.join(codex, "config.toml"), "utf8")).includes(base),
  );
  assert.deepEqual(await files("claude_cli"), cliBefore);
  assert.equal((await status("claude_cli")).state, "applied");
  checks.push(
    "关闭正在使用的Codex转换原子恢复原配置并提示重启，Claude原生客户端文件和状态保持正常",
  );

  await selectClaude("claude_cli");
  assert.equal(
    await row(claude.name)
      .getByRole("switch", { name: /转换为/ })
      .count(),
    0,
  );
  assert.ok(
    await row(openai.name)
      .getByRole("switch", { name: "转换为 Claude · OpenAI 兼容供应商" })
      .isChecked(),
  );
  await row(openai.name)
    .getByRole("button", { name: "使用", exact: true })
    .click();
  await until(
    async () => (await status("claude_cli")).activeProviderId === openai.id,
  );
  const settings = JSON.parse(
    await readFile(path.join(cli, "settings.json"), "utf8"),
  );
  assert.ok(
    settings.env.ANTHROPIC_BASE_URL.includes(`/claude/claude_cli/${openai.id}`),
  );
  const reverse = await fetch(
    `${settings.env.ANTHROPIC_BASE_URL}/v1/messages`,
    {
      method: "POST",
      headers: {
        Authorization: `Bearer ${settings.env.ANTHROPIC_AUTH_TOKEN}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        model: "claude-sonnet-4-6",
        messages: [{ role: "user", content: "Hello" }],
        max_tokens: 512,
        stream: false,
      }),
    },
  );
  assert.equal(reverse.status, 200);
  assert.equal(
    (await reverse.json()).content[0].text,
    "OpenAI converted reply",
  );
  await changeConversion(openai.name, false);
  const restoredCli = JSON.parse(
    await readFile(path.join(cli, "settings.json"), "utf8"),
  );
  assert.deepEqual(restoredCli, originalCli);
  assert.equal((await status("claude_cli")).activeProviderId, null);
  await assert.rejects(
    invoke("apply_provider", { target: "claude_cli", providerId: openai.id }),
    (e) => e.code === "conversion_required",
  );
  checks.push(
    "Claude CLI使用OpenAI时实际Messages转Responses成功；关闭恢复接管前原配置并禁止再次应用",
  );

  await selectClaude("claude_desktop");
  assert.ok(
    await row(openai.name)
      .getByRole("switch", { name: "转换为 Claude · OpenAI 兼容供应商" })
      .isChecked(),
  );
  await row(openai.name)
    .getByRole("button", { name: "使用", exact: true })
    .click();
  await until(
    async () => (await status("claude_desktop")).activeProviderId === openai.id,
  );
  const desktop = await files("claude_desktop");
  const profileFile = desktop.find(([p]) => /configLibrary.*e82de475/.test(p));
  const profile = JSON.parse(profileFile[1]);
  assert.ok(
    profile.inferenceGatewayBaseUrl.includes(
      `/claude/claude_desktop/${openai.id}`,
    ),
  );
  assert.equal(profile.inferenceModels[0].labelOverride, "claude-sonnet-4-6");
  const desktopResponse = await fetch(
    `${profile.inferenceGatewayBaseUrl}/v1/messages`,
    {
      method: "POST",
      headers: {
        Authorization: `Bearer ${profile.inferenceGatewayApiKey}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        model: profile.inferenceModels[0].name,
        messages: [{ role: "user", content: "Hello" }],
        max_tokens: 512,
        stream: false,
      }),
    },
  );
  assert.equal(desktopResponse.status, 200);
  assert.equal(
    (await desktopResponse.json()).content[0].text,
    "OpenAI converted reply",
  );
  assert.equal(inferenceRequests.at(-1).model, "claude-sonnet-4-6");
  await audit("Claude desktop conversion list");
  await verifyCenteredPromotion(page);
  await page.screenshot({
    path: "docs/screenshots/protocol-controls-claude-local.png",
  });
  await changeConversion(openai.name, false);
  const restoredDesktop = await files("claude_desktop");
  for (const [file, original] of desktopOriginals) {
    const actual = restoredDesktop.find(([p]) => p === file)[1];
    assert.deepEqual(
      actual === null ? null : JSON.parse(actual),
      original === null ? null : JSON.parse(original),
    );
  }
  const optouts = (await saved(openai.id)).codexOptions
    .conversionDisabledTargets;
  assert.ok(
    optouts.includes("claude_cli") && optouts.includes("claude_desktop"),
  );
  await changeConversion(openai.name, true);
  assert.deepEqual(
    (await saved(openai.id)).codexOptions.conversionDisabledTargets,
    ["claude_cli"],
  );
  assert.equal((await status("claude_desktop")).activeProviderId, null);
  checks.push(
    "Claude桌面写入独立转换地址与模型别名，关闭完整恢复原文件；桌面和CLI开关独立保存",
  );

  for (const size of [
    { width: 1120, height: 780 },
    { width: 1001, height: 780 },
    { width: 1000, height: 780 },
    { width: 920, height: 720 },
    { width: 760, height: 600 },
    { width: 390, height: 620 },
  ]) {
    await page.setViewportSize(size);
    await verifyCenteredPromotion(page);
    assert.equal(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
      false,
    );
    assert.equal(
      await row(openai.name).evaluate((el) => el.scrollWidth > el.clientWidth),
      false,
    );
    await audit(`Protocol controls ${size.width}`);
  }
  await page.screenshot({
    path: "docs/screenshots/protocol-controls-narrow-local.png",
  });
  checks.push(
    "协议、开关与现有模型配置保持平铺；桌面及390px窄窗无横向溢出，Axe检查无违规",
  );
  assert.deepEqual(errors, []);
  const result = {
    root,
    checks,
    audits,
    pageErrors: errors,
    modelRequests,
    inferenceRequests,
  };
  await writeFile(
    path.join(root, "results.json"),
    JSON.stringify(result, null, 2),
  );
  await writeFile(
    path.resolve(".qa/protocol-controls/results.json"),
    JSON.stringify(result, null, 2),
  );
  console.log(JSON.stringify(result, null, 2));
} catch (error) {
  if (page)
    await page
      .screenshot({ path: path.join(root, "failure.png") })
      .catch(() => {});
  throw error;
} finally {
  if (browser) await browser.close();
  if (app && app.exitCode === null)
    await new Promise((resolve) => {
      app.once("exit", resolve);
      app.kill();
    });
  await new Promise((resolve) => server.close(resolve));
}
