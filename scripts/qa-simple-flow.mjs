import { chromium } from "playwright";
import AxeBuilder from "@axe-core/playwright";
import http from "node:http";
import net from "node:net";
import { spawn } from "node:child_process";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import assert from "node:assert/strict";
import {
  mockModelResponse,
  verifyCodexModels,
} from "./qa-codex-model-runtime.mjs";

const root = path.resolve(".qa/simple-flow/native", String(Date.now()));
const home = path.join(root, "用户 中文");
const codex = path.join(home, "Codex 实际目录");
const local = path.join(home, "Local");
const cli = path.join(home, ".claude");
const contextFile = path.join(root, "context.json");
await mkdir(codex, { recursive: true });
await mkdir(local, { recursive: true });
await mkdir(cli, { recursive: true });
await mkdir("docs/screenshots", { recursive: true });
await writeFile(
  path.join(codex, "config.toml"),
  "# isolated original\nmodel='original'\n",
);
await writeFile(
  contextFile,
  JSON.stringify({
    home,
    local,
    roaming: null,
    roots: [home],
    notes: [],
    hints: [
      {
        target: "codex",
        directory: codex,
        evidence: "隔离测试：运行中的客户端",
        running: true,
      },
    ],
  }),
);
const key = "isolated-simple-flow-key",
  requests = [],
  inference = [],
  checks = [],
  audits = [],
  errors = [];
let denied = false;
const server = http.createServer((req, res) => {
  const url = new URL(req.url, "http://isolated.test");
  const site = url.pathname.split("/")[1];
  const send = (data, code = 200) => {
    res.writeHead(code, { "Content-Type": "application/json" });
    res.end(JSON.stringify(data));
  };
  if (url.pathname.endsWith("/responses"))
    return void mockModelResponse(req, res, inference);
  requests.push({
    site,
    method: req.method,
    path: url.pathname,
    bearer: req.headers.authorization === `Bearer ${key}`,
    native: req.headers["x-api-key"] === key,
  });
  if (url.pathname.endsWith("/models")) {
    if (denied || req.headers.authorization === "Bearer bad-key")
      return send({ error: "do-not-expose-private-key" }, 403);
    if (site === "claude") {
      if (req.headers["x-api-key"] !== key) return send({}, 401);
      return send({
        data: [
          { id: "claude-opus-4-6", type: "model" },
          { id: "claude-sonnet-4-6", type: "model" },
        ],
        has_more: false,
      });
    }
    return setTimeout(
      () =>
        send({
          object: "list",
          data: ["gpt-5.5", "gpt-6.1-sol", "text-embedding-3-large"].map(
            (id) => ({ id, object: "model" }),
          ),
        }),
      180,
    );
  }
  if (url.pathname.endsWith("/usage"))
    return send({ mode: "unrestricted", balance: 12.5, unit: "USD" });
  return send({}, 404);
});
await new Promise((r) => server.listen(0, "127.0.0.1", r));
const origin = `http://127.0.0.1:${server.address().port}`;
const base = (site) => `${origin}/${site}/v1`;
const until = async (fn, timeout = 20000) => {
  const end = Date.now() + timeout;
  while (Date.now() < end) {
    if (await fn()) return;
    await new Promise((r) => setTimeout(r, 100));
  }
  throw Error("Simple flow QA timeout");
};
const portOpen = async () => {
  try {
    return (await fetch("http://127.0.0.1:9223/json/version")).ok;
  } catch {
    return false;
  }
};
let child, browser, page, occupied;
const invoke = async (name, args = {}) => {
  const response = await page.evaluate(
    async ({ name, args }) => {
      try {
        return { value: await window.__TAURI_INTERNALS__.invoke(name, args) };
      } catch (error) {
        return { error };
      }
    },
    { name, args },
  );
  if (response.error)
    throw Object.assign(new Error(response.error.message), response.error);
  return response.value;
};
const overview = () => invoke("get_overview");
const launch = async () => {
  assert.equal(await portOpen(), false, "QA port belongs to no other process");
  child = spawn(
    path.resolve(
      process.env.UNI_SWITCH_QA_EXE ||
        ".qa/simple-flow/test-app/uni-switch.exe",
    ),
    [],
    {
      windowsHide: true,
      stdio: "ignore",
      env: {
        ...process.env,
        UNI_SWITCH_DATA_DIR: path.join(root, "data"),
        UNI_SWITCH_QA_DISCOVERY_CONTEXT: contextFile,
        WEBVIEW2_USER_DATA_FOLDER: path.join(root, "webview"),
        CODEX_HOME: path.join(home, ".codex"),
        CLAUDE_CONFIG_DIR: cli,
        LOCALAPPDATA: local,
      },
    },
  );
  await until(portOpen);
  browser = await chromium.connectOverCDP("http://127.0.0.1:9223");
  page = browser.contexts()[0].pages()[0];
  page.setDefaultTimeout(15000);
  page.on("pageerror", (error) => errors.push(error.message));
  await page.getByRole("button", { name: "设置", exact: true }).waitFor();
};
const stop = async () => {
  if (browser) await browser.close();
  browser = null;
  if (child && child.exitCode === null)
    await new Promise((resolve) => {
      child.once("exit", resolve);
      child.kill();
    });
  child = null;
  await until(async () => !(await portOpen()));
};
const audit = async (label) => {
  await page.evaluate(() =>
    Promise.all(
      document
        .getAnimations()
        .filter((a) => Number.isFinite(a.effect?.getTiming().iterations))
        .map((a) => a.finished.catch(() => {})),
    ),
  );
  const result = await new AxeBuilder({ page }).analyze();
  audits.push({
    label,
    violations: result.violations.map((v) => ({
      id: v.id,
      targets: v.nodes.map((n) => n.target),
    })),
  });
  assert.deepEqual(audits.at(-1).violations, [], label);
};
const add = async (site) => {
  await page.getByRole("button", { name: /^添加(第一个)?供应商$/ }).click();
  await page.locator("#baseUrl").fill(base(site));
  await page.locator("#apiKey").fill(key);
};
const advanced = () => page.locator(".form-advanced > summary").click();
const close = () =>
  page.getByRole("button", { name: "取消", exact: true }).click();
const row = (p) =>
  page
    .locator(".provider-card")
    .filter({ has: page.getByText(p.baseUrl, { exact: true }) });
const edit = async (p) => {
  await row(p).locator(".provider-more > summary").click();
  await row(p)
    .getByRole("button", { name: `编辑 ${p.name}`, exact: true })
    .click();
};
const use = async (p) => {
  await row(p).getByRole("button", { name: "使用", exact: true }).click();
  await row(p).getByRole("button", { name: "使用中", exact: true }).waitFor();
};
const snapshot = async () => ({
  providers: JSON.stringify((await overview()).providers),
  config: await readFile(path.join(codex, "config.toml"), "utf8"),
  catalog: await readFile(path.join(codex, "uni-switch-models.json"), "utf8"),
  database: (await readFile(path.join(root, "data/uni-switch.db"))).toString(
    "base64",
  ),
  journal: (await readFile(path.join(root, "data/uni-switch.db-wal"))).toString(
    "base64",
  ),
});
try {
  await launch();
  await until(
    async () =>
      path
        .resolve(
          (await overview()).targets.find((t) => t.target === "codex")
            .directory,
        )
        .toLowerCase() === codex.toLowerCase(),
  );
  assert.equal(
    await readFile(path.join(codex, "config.toml"), "utf8"),
    "# isolated original\nmodel='original'\n",
  );
  console.log(`QA ${checks.length + 1}`);
  checks.push("启动即识别实际配置目录，未接入时不写客户端配置");

  await add("gpt");
  assert.equal(
    await page.locator('input:visible:not([type="checkbox"])').count(),
    2,
  );
  assert.equal(await page.locator('button[type="submit"]:visible').count(), 1);
  assert.equal(await page.getByLabel("API 协议").isVisible(), false);
  await audit("Two-field compact form");
  await page.screenshot({ path: "docs/screenshots/simple-add-form.png" });
  await page.getByRole("button", { name: "添加并使用", exact: true }).click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  let data = await overview(),
    gpt = data.providers[0];
  assert.equal(gpt.model, "gpt-6.1-sol");
  assert.equal(gpt.codexOptions.upstreamProtocol, "openai");
  assert.equal(gpt.codexOptions.models.filter((m) => m.enabled).length, 1);
  assert.equal(
    requests.filter((r) => r.site === "gpt" && r.path.endsWith("/models"))
      .length,
    1,
  );
  assert.equal(inference.length, 0, "Discovery never sends paid inference");
  await row(gpt)
    .locator(".balance-details summary")
    .getByText(/12.5/)
    .waitFor();
  console.log(`QA ${checks.length + 1}`);
  checks.push(
    "两字段一次主按钮自动取得模型、命名、余额并使用；无推理探测或重复模型请求",
  );

  const unchanged = await snapshot();
  await edit(gpt);
  await advanced();
  await page
    .getByRole("checkbox", { name: "启用 gpt-5.5", exact: true })
    .check();
  await page.getByLabel("上下文长度 gpt-6.1-sol", { exact: true }).fill("512");
  await page.getByLabel("Fast 模式").selectOption("on");
  await page
    .getByRole("button", { name: "修复思考强度列表", exact: true })
    .click();
  await page.locator("#apiKey").fill("unconfirmed-key");
  await page.locator("#baseUrl").fill(base("changed-draft"));
  await page.waitForTimeout(900);
  await close();
  assert.deepEqual(
    await snapshot(),
    unchanged,
    "Cancel cannot change DB, credentials, catalog or client bytes",
  );
  await row(gpt).locator(".provider-more > summary").focus();
  await page.keyboard.press("Enter");
  await audit("Supplier more menu");
  await page.keyboard.press("Escape");
  assert.equal(
    await row(gpt).locator(".provider-more").getAttribute("open"),
    null,
  );
  console.log(`QA ${checks.length + 1}`);
  checks.push(
    "修改密钥、地址、模型、上下文、Fast和修复后取消，数据库及客户端文件逐字节不变；菜单支持Escape",
  );

  await edit(gpt);
  await advanced();
  await page
    .getByRole("checkbox", { name: "启用 gpt-5.5", exact: true })
    .check();
  await page.getByLabel("上下文长度 gpt-5.5", { exact: true }).fill("128");
  await page.getByLabel("上下文长度 gpt-6.1-sol", { exact: true }).fill("512");
  await page
    .getByRole("button", { name: "修复思考强度列表", exact: true })
    .click();
  await audit("Advanced draft form");
  await page.setViewportSize({ width: 390, height: 844 });
  await page
    .getByLabel("上下文长度 gpt-6.1-sol", { exact: true })
    .scrollIntoViewIfNeeded();
  assert.equal(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    ),
    false,
  );
  await audit("Narrow advanced form");
  await page.screenshot({
    path: "docs/screenshots/simple-advanced-narrow.png",
  });
  await page.setViewportSize({ width: 1120, height: 780 });
  await page.getByRole("button", { name: "保存并使用", exact: true }).click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  const catalog = JSON.parse(
    await readFile(path.join(codex, "uni-switch-models.json"), "utf8"),
  );
  for (const model of catalog.models) {
    assert.equal(
      model.context_window,
      model.slug === "gpt-5.5" ? 128000 : 512000,
    );
    assert.equal(model.auto_compact_token_limit, model.context_window);
    assert.ok(model.supported_reasoning_levels.some((e) => e.effort === "max"));
  }
  await verifyCodexModels({
    codexHome: codex,
    expected: { "gpt-5.5": 128000, "gpt-6.1-sol": 512000 },
    output: path.join(root, "codex-runtime.json"),
  });
  console.log(`QA ${checks.length + 1}`);
  checks.push(
    "模型和长度确认后一起写入；真实Codex读取128k/512k并切换模型，思考列表含max",
  );

  await add("claude");
  await page.locator("#apiKey").press("Enter");
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  data = await overview();
  const claude = data.providers.find((p) => p.baseUrl === base("claude"));
  assert.equal(claude.model, "claude-sonnet-4-6");
  assert.equal(claude.authMode, "x-api-key");
  assert.equal(claude.codexOptions.upstreamProtocol, "anthropic");
  assert.ok(
    (await readFile(path.join(codex, "config.toml"), "utf8")).includes(
      "http://127.0.0.1:",
    ),
  );
  assert.equal(
    requests.filter((r) => r.site === "claude" && r.path.endsWith("/models"))
      .length,
    2,
  );
  await use(gpt);
  console.log(`QA ${checks.length + 1}`);
  checks.push(
    "Enter也可完成首次接入；Claude认证失败自动改用x-api-key，用于Codex自动启动转换；切换只点一次使用",
  );

  await page.getByRole("tab", { name: /Claude Code/ }).click();
  assert.equal(await page.locator(".provider-card").count(), 2);
  await use(gpt);
  data = await overview();
  const desktop = data.targets.find((t) => t.target === "claude_desktop");
  const profile = JSON.parse(
    await readFile(
      desktop.files.find((f) => f.includes("e82de475") && f.endsWith(".json")),
      "utf8",
    ),
  );
  assert.ok(
    profile.inferenceModels.every((m) => m.labelOverride.startsWith("gpt-")),
  );
  await page.getByRole("button", { name: "CLI", exact: true }).click();
  await use(gpt);
  assert.equal(
    JSON.parse(await readFile(path.join(cli, "settings.json"), "utf8")).env
      .ANTHROPIC_MODEL,
    "gpt-6.1-sol",
  );
  data = await overview();
  assert.equal(data.providers.length, 2);
  assert.ok(data.targets.every((t) => t.activeProviderId === gpt.id));
  console.log(`QA ${checks.length + 1}`);
  checks.push(
    "两端同一供应商ID直接复用，三个目标分别使用；Claude模型菜单自动显示真实GPT名称",
  );

  await page.getByRole("tab", { name: /Codex/ }).click();
  const clean = await readFile(path.join(codex, "config.toml"), "utf8");
  await writeFile(
    path.join(codex, "config.toml"),
    clean.replace(base("gpt"), base("external")),
  );
  const beforeFail = await snapshot();
  await edit(gpt);
  await page.locator("#apiKey").fill("edited-key-that-cannot-commit");
  await page.getByRole("button", { name: "保存并使用", exact: true }).click();
  await page
    .getByRole("alert")
    .getByText(/外部修改|其他工具/)
    .waitFor();
  assert.deepEqual(await snapshot(), beforeFail);
  await writeFile(path.join(codex, "config.toml"), clean);
  await page.getByRole("button", { name: "保存并使用", exact: true }).click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  data = await overview();
  assert.equal(data.providers.length, 2);
  assert.equal(data.providers.find((p) => p.id === gpt.id).keySuffix, "mmit");
  assert.equal(
    data.targets.find((t) => t.target === "claude_cli").state,
    "saved_changes",
  );
  assert.equal(
    JSON.parse(
      await readFile(
        path.join(root, "data/bridge-claude-cli-active.json"),
        "utf8",
      ),
    ).api_key,
    key,
  );
  console.log(`QA ${checks.length + 1}`);
  checks.push(
    "外部修改使确认失败时，原数据不变；重试保留共享ID，其他客户端保持原生效密钥",
  );
  // Restore the fake upstream key through the same confirmed UI path.
  await edit(data.providers.find((p) => p.id === gpt.id));
  await page.locator("#apiKey").fill(key);
  await page.getByRole("button", { name: "保存并使用", exact: true }).click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  await page.getByRole("tab", { name: /Claude Code/ }).click();
  await page.getByRole("button", { name: "CLI", exact: true }).click();
  await stop();

  const route = JSON.parse(
    await readFile(path.join(root, "data/protocol-bridge.json"), "utf8"),
  );
  occupied = net.createServer((socket) => {
    socket.resume();
    socket.end("HTTP/1.1 401 Unauthorized\r\nContent-Length: 0\r\n\r\n");
  });
  await new Promise((r) => occupied.listen(route.port, "127.0.0.1", r));
  await launch();
  assert.equal(
    await page
      .getByRole("tab", { name: /Claude Code/ })
      .getAttribute("data-state"),
    "active",
  );
  assert.equal(
    await page
      .getByRole("button", { name: "CLI", exact: true })
      .getAttribute("aria-pressed"),
    "true",
  );
  assert.equal(
    (await invoke("get_runtime_status", { target: "claude_cli" }))
      .bridgeHealthy,
    false,
  );
  const blocked = await snapshot();
  await assert.rejects(
    invoke("apply_provider", { target: "claude_cli", providerId: gpt.id }),
    (e) => e.code === "bridge_unavailable",
  );
  assert.deepEqual(await snapshot(), blocked);
  await new Promise((r) => occupied.close(r));
  occupied = null;
  await until(
    async () =>
      (await invoke("get_runtime_status", { target: "claude_cli" }))
        .bridgeHealthy,
  );
  const recovered = JSON.parse(
    await readFile(path.join(root, "data/protocol-bridge.json"), "utf8"),
  );
  assert.deepEqual(recovered, route);
  await invoke("apply_provider", { target: "claude_cli", providerId: gpt.id });
  console.log(`QA ${checks.length + 1}`);
  checks.push(
    "重开记住Claude CLI；转换端口占用时应用仍启动，阻止错误使用，释放后自动恢复同一端口和令牌",
  );

  await page.getByRole("tab", { name: /Codex/ }).click();
  await audit("Shared supplier list");
  await page.screenshot({ path: "docs/screenshots/simple-supplier-list.png" });
  await page.setViewportSize({ width: 390, height: 844 });
  assert.equal(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    ),
    false,
  );
  await audit("Narrow supplier list");
  await row(gpt).locator(".provider-more > summary").click();
  await audit("Narrow more menu");
  const menuBounds = await row(gpt)
    .locator(".provider-more-content")
    .evaluate((el) => ({
      width: el.clientWidth,
      contentWidth: el.scrollWidth,
      top: el.getBoundingClientRect().top,
      bottom: el.getBoundingClientRect().bottom,
      viewport: innerHeight,
    }));
  assert.ok(
    menuBounds.contentWidth <= menuBounds.width,
    "More menu has no internal horizontal overflow",
  );
  assert.ok(
    menuBounds.top >= 0 && menuBounds.bottom <= menuBounds.viewport,
    "More menu stays within viewport",
  );
  await page.screenshot({ path: "docs/screenshots/simple-list-narrow.png" });
  console.log(`QA ${checks.length + 1}`);
  checks.push("精简列表、更多菜单及390px表单/列表无横向溢出，7项Axe检查无违规");
  assert.deepEqual(errors, []);
  const result = {
    version: "0.4.0",
    passed: checks.length,
    checks,
    audits,
    requests,
    pageErrors: errors,
    root,
  };
  await writeFile(
    path.join(root, "results.json"),
    JSON.stringify(result, null, 2),
  );
  await writeFile(
    path.resolve(".qa/simple-flow/results.json"),
    JSON.stringify(result, null, 2),
  );
  console.log(JSON.stringify({ passed: checks.length, checks }, null, 2));
} catch (error) {
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
  throw error;
} finally {
  if (browser || child) await stop();
  if (occupied) await new Promise((r) => occupied.close(r));
  await new Promise((r) => server.close(r));
}
