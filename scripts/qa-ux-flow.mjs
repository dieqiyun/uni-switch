import { chromium } from "playwright";
import AxeBuilder from "@axe-core/playwright";
import http from "node:http";
import net from "node:net";
import { spawn } from "node:child_process";
import { mkdir, readFile, writeFile, copyFile } from "node:fs/promises";
import path from "node:path";
import assert from "node:assert/strict";
import { verifyWindowChrome } from "./qa-window-chrome.mjs";
import { verifyInlineModels } from "./qa-inline-models.mjs";
import { verifyProviderLayout } from "./qa-provider-layout.mjs";
import {
  mockModelResponse,
  verifyCodexModels,
} from "./qa-codex-model-runtime.mjs";

const root = path.resolve(".qa/ux-flow/native", String(Date.now()));
const fixtureImage = path.join(
  root,
  "WindowsApps",
  "OpenAI.Codex_qa",
  "app",
  "ChatGPT.exe",
);
const fixtureUserData = path.join(root, "Codex 用户数据");
const fixtureMarker = path.join(fixtureUserData, "restart-fixture.json");
let fixture, fixturePid;
let currentFixtureImage = fixtureImage;
const cliFixtureImage = path.join(root, "CLI 程序", "Claude.exe");
const cliWorkingDirectory = path.join(root, "项目 中文 & [测试]");
const cliFixtureMarker = path.join(cliWorkingDirectory, "cli-fixture.json");
const cliFixturePids = new Set();
const stopCliFixtures = async () => {
  // All recorded PIDs belong to our isolated fixture, which exits on a marker.
  for (let attempt = 0; attempt < 5; attempt++) {
    try {
      cliFixturePids.add(
        JSON.parse(await readFile(cliFixtureMarker, "utf8")).pid,
      );
    } catch {}
    for (const pid of cliFixturePids) {
      await writeFile(
        cliFixtureMarker.replace(/\.json$/, `.exit-${pid}`),
        "exit",
      ).catch(() => {});
    }
    if (cliFixturePids.size) await new Promise((r) => setTimeout(r, 600));
  }
};
const killFixture = async () => {
  if (fixture && fixture.exitCode === null) {
    await new Promise((resolve) => {
      fixture.once("exit", resolve);
      fixture.kill();
    });
  }
  fixture = null;
  if (fixturePid) {
    const clean = spawn(
      "powershell.exe",
      [
        "-NoProfile",
        "-Command",
        `$taskProcess = Get-Process -Id ${fixturePid} -ErrorAction SilentlyContinue; if ($taskProcess -and $taskProcess.Path -eq $env:UNI_SWITCH_EXPECTED_FIXTURE) { Stop-Process -Id $taskProcess.Id }`,
      ],
      {
        windowsHide: true,
        stdio: "ignore",
        env: {
          ...process.env,
          UNI_SWITCH_EXPECTED_FIXTURE: currentFixtureImage,
        },
      },
    );
    await new Promise((resolve) => clean.once("exit", resolve));
    fixturePid = null;
  }
};
const home = path.join(root, "用户 中文");
const codex = path.join(home, ".codex");
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
const configCopy = path.join(codex, "browser", "config.toml");
await mkdir(path.dirname(configCopy), { recursive: true });
await writeFile(configCopy, "# isolated browser copy\nmodel='copy-model'\n");
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
const key = "isolated-ux-flow-key",
  requests = [],
  inference = [],
  checks = [],
  audits = [],
  errors = [];
let denied = false;
let hideDefault = false;
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
    if (site === "many")
      return send({
        object: "list",
        data: Array.from({ length: 85 }, (_, i) => ({
          id: `gpt-${i}`,
          object: "model",
        })),
      });
    if (site === "slow")
      return setTimeout(
        () =>
          send({ object: "list", data: [{ id: "gpt-5.5", object: "model" }] }),
        3000,
      );
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
          data: ["gpt-5.5", "gpt-6.1-sol", "text-embedding-3-large"]
            .filter((id) => !hideDefault || id !== "gpt-6.1-sol")
            .map((id) => ({ id, object: "model" })),
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
      process.env.UNI_SWITCH_QA_EXE || ".qa/ux-flow/test-app/uni-switch.exe",
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
        CODEX_HOME: codex,
        CLAUDE_CONFIG_DIR: cli,
        LOCALAPPDATA: local,
        UNI_SWITCH_CLI_FIXTURE_MARKER: cliFixtureMarker,
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
  console.log("Audit start", label);
  await page.emulateMedia({ reducedMotion: "reduce" });
  const result = await Promise.race([
    new AxeBuilder({ page }).analyze(),
    new Promise((_, reject) =>
      setTimeout(() => reject(new Error("Axe timeout: " + label)), 30000),
    ),
  ]);
  audits.push({
    label,
    violations: result.violations.map((v) => ({
      id: v.id,
      targets: v.nodes.map((n) => n.target),
    })),
  });
  assert.deepEqual(audits.at(-1).violations, [], label);
  console.log("Audit passed", label);
};
const add = async (site) => {
  await page.getByRole("button", { name: /^添加(第一个)?供应商$/ }).click();
  await page.locator("#baseUrl").fill(base(site));
  await page.locator("#apiKey").fill(key);
};
const advanced = async () => {
  assert.equal(
    await page
      .getByRole("group", { name: "API 协议", exact: true })
      .isVisible(),
    true,
  );
  assert.equal(await page.locator(".form-advanced > summary").count(), 0);
  assert.equal(await page.locator(".model-settings").isVisible(), true);
  assert.equal(await page.locator(".model-settings summary").count(), 0);
};
const dismissCodexWrite = async () => {
  const dialog = page.getByRole("dialog", {
    name: "重启 Codex 使配置生效",
    exact: true,
  });
  await dialog.waitFor();
  await dialog.getByRole("button", { name: "稍后重启", exact: true }).click();
  await dialog.waitFor({ state: "hidden" });
};
const finishFormWrite = async () => {
  await page
    .getByRole("dialog", { name: /^(添加供应商|编辑供应商)$/ })
    .waitFor({ state: "hidden" });
  await dismissCodexWrite();
};
const close = () =>
  page.getByRole("button", { name: "取消", exact: true }).click();
const row = (p) =>
  page
    .locator(".provider-card")
    .filter({ has: page.getByText(p.baseUrl, { exact: true }) });
const edit = async (p) => {
  await row(p)
    .getByRole("button", { name: `编辑 ${p.name}`, exact: true })
    .click();
};
const use = async (p) => {
  const selectedTarget =
    (await page
      .getByRole("tab", { name: /Codex/ })
      .getAttribute("data-state")) === "active"
      ? "codex"
      : (await page
            .getByRole("button", { name: "CLI", exact: true })
            .getAttribute("aria-pressed")) === "true"
        ? "claude_cli"
        : "claude_desktop";
  const before = (await overview()).targets.find(
    (t) => t.target === "codex",
  ).configurationRevision;
  await row(p).getByRole("button", { name: "使用", exact: true }).click();
  await until(
    async () =>
      (await overview()).targets.find((t) => t.target === selectedTarget)
        .activeProviderId === p.id,
  );
  const after = (await overview()).targets.find(
    (t) => t.target === "codex",
  ).configurationRevision;
  if (after > before) await dismissCodexWrite();
  await row(p)
    .getByRole("button", { name: /^(已应用|已配置|待重启)$/ })
    .waitFor();
};
const snapshot = async () => ({
  providers: JSON.stringify((await overview()).providers),
  config: await readFile(path.join(codex, "config.toml"), "utf8"),
  catalog: await readFile(
    path.join(codex, "uni-switch-models.json"),
    "utf8",
  ).catch((error) => {
    if (error.code === "ENOENT") return null;
    throw error;
  }),
  database: (await readFile(path.join(root, "data/uni-switch.db"))).toString(
    "base64",
  ),
  journal: (await readFile(path.join(root, "data/uni-switch.db-wal"))).toString(
    "base64",
  ),
});
try {
  await launch();
  const skippedChecks = await verifyWindowChrome({
    page,
    child,
    invoke,
    checks,
  });
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
  checks.push(
    "直接使用配置目录，browser副本不触发搜索或确认，未接入时不写客户端配置",
  );
  assert.equal(
    await page.getByText("确认配置位置", { exact: true }).count(),
    0,
  );
  assert.equal(await page.getByText(codex, { exact: true }).count(), 0);
  await assert.rejects(invoke("check_config_directory", { target: "codex" }));
  await assert.rejects(
    invoke("discover_config_directories", {
      target: "codex",
      searchRoot: null,
    }),
  );
  await page.getByRole("button", { name: "设置", exact: true }).click();
  await page.getByText(codex, { exact: true }).waitFor();
  assert.equal(
    await page.getByRole("button", { name: "自动查找", exact: true }).count(),
    0,
  );
  await audit("Configuration location is only in settings");
  await page.screenshot({
    path: "docs/screenshots/settings-location-0.5.3.png",
  });
  await page.getByRole("button", { name: "修改目录", exact: true }).click();
  await page
    .getByLabel("配置目录", { exact: true })
    .fill(path.join(home, "unconfirmed-directory"));
  await page.getByRole("button", { name: "取消", exact: true }).click();
  assert.equal(
    (await overview()).targets.find((t) => t.target === "codex").directory,
    codex,
  );
  await page.getByText(codex, { exact: true }).waitFor();
  await page.getByRole("button", { name: "修改目录", exact: true }).click();
  const manualDirectory = path.join(home, "manual-directory");
  await page.getByLabel("配置目录", { exact: true }).fill(manualDirectory);
  await page.getByRole("button", { name: "保存目录", exact: true }).click();
  await page.getByText(manualDirectory, { exact: true }).waitFor();
  assert.equal(
    (await overview()).targets.find((t) => t.target === "codex").directory,
    manualDirectory,
  );
  await page.getByRole("button", { name: "修改目录", exact: true }).click();
  await page.getByLabel("配置目录", { exact: true }).fill(codex);
  await page.getByRole("button", { name: "保存目录", exact: true }).click();
  await page.getByText(codex, { exact: true }).waitFor();
  await page.getByRole("button", { name: "关闭弹窗", exact: true }).click();
  checks.push(
    "配置位置只在设置显示，手动修改取消不保存，确认后返回设置并记住目录",
  );

  await add("gpt");
  assert.equal(await page.locator("input:visible[required]").count(), 2);
  assert.equal(await page.locator('button[type="submit"]:visible').count(), 1);
  assert.equal(
    await page
      .getByRole("group", { name: "API 协议", exact: true })
      .isVisible(),
    true,
  );
  assert.equal(
    await page
      .getByRole("group", { name: "认证方式", exact: true })
      .isVisible(),
    true,
  );
  assert.equal(
    await page
      .getByRole("group", { name: "Fast 模式", exact: true })
      .isVisible(),
    true,
  );
  assert.equal(await page.locator(".form-advanced > summary").count(), 0);
  assert.equal(await page.locator(".model-settings").isVisible(), true);
  assert.equal(await page.locator(".model-settings summary").count(), 0);
  assert.equal(await page.getByRole("dialog").locator("select").count(), 0);
  assert.equal(
    await page
      .getByRole("group", { name: "API 协议", exact: true })
      .getByRole("radio", { name: "自动匹配", exact: true })
      .isChecked(),
    true,
  );
  assert.equal(
    await page
      .getByRole("group", { name: "认证方式", exact: true })
      .getByRole("radio", { name: "自动匹配", exact: true })
      .isChecked(),
    true,
  );
  assert.equal(
    await page
      .getByRole("radio", { name: "跟随客户端", exact: true })
      .isChecked(),
    true,
  );
  await audit("Two required fields with visible advanced settings");
  await page
    .getByRole("checkbox", { name: "启用 gpt-6.1-sol", exact: true })
    .waitFor();
  await page.evaluate(() => document.activeElement?.blur());
  await page.screenshot({ path: "docs/screenshots/ux-add-form.png" });
  await page.getByRole("button", { name: "添加并使用", exact: true }).click();
  await page
    .getByRole("dialog", { name: "添加供应商", exact: true })
    .waitFor({ state: "hidden" });
  const firstWriteDialog = page.getByRole("dialog", {
    name: "重启 Codex 使配置生效",
    exact: true,
  });
  await firstWriteDialog.waitFor();
  assert.equal(
    await firstWriteDialog
      .getByRole("button", { name: "立即重启", exact: true })
      .isDisabled(),
    true,
  );
  await audit("Codex write reminder without a running desktop");
  await page.screenshot({
    path: "docs/screenshots/codex-write-reminder-0.5.6.png",
  });
  await dismissCodexWrite();
  checks.push(
    "未运行Codex桌面端时，配置实际写入仍弹重启提示，自动重启不可用时提供手动说明",
  );
  let data = await overview(),
    gpt = data.providers[0];
  assert.equal(gpt.model, "gpt-6.1-sol");
  assert.equal(gpt.codexOptions.upstreamProtocol, "openai");
  assert.deepEqual(
    gpt.codexOptions.models
      .filter((m) => m.enabled)
      .map((m) => m.id)
      .sort(),
    ["gpt-5.5", "gpt-6.1-sol"],
  );
  const firstCatalog = JSON.parse(
    await readFile(path.join(codex, "uni-switch-models.json"), "utf8"),
  );
  assert.deepEqual(firstCatalog.models.map((m) => m.slug).sort(), [
    "gpt-5.5",
    "gpt-6.1-sol",
  ]);
  assert.equal(
    requests.filter((r) => r.site === "gpt" && r.path.endsWith("/models"))
      .length,
    1,
  );
  assert.equal(inference.length, 0, "Discovery never sends paid inference");
  await row(gpt).locator(".balance-summary").getByText(/12.5/).waitFor();
  const balanceInfo = row(gpt).getByRole("button", {
    name: `查看 ${gpt.name} 余额详情`,
    exact: true,
  });
  assert.equal(
    await page.locator(".provider-list select, .provider-list details").count(),
    0,
  );
  await balanceInfo.focus();
  await page.keyboard.press("Enter");
  assert.equal(await balanceInfo.getAttribute("aria-expanded"), "true");
  await row(gpt)
    .locator(".balance-detail-content")
    .getByText(/查询时间/)
    .waitFor();
  await audit("Flat balance details keyboard flow");
  await page.keyboard.press("Enter");
  assert.equal(await balanceInfo.getAttribute("aria-expanded"), "false");
  console.log(`QA ${checks.length + 1}`);
  checks.push(
    "两字段一次主按钮自动取得模型并默认全选写入Codex目录、命名、余额并使用；无推理探测或重复模型请求",
  );

  const fast = row(gpt).getByRole("switch", {
    name: `Fast 模式 · ${gpt.name}`,
    exact: true,
  });
  assert.equal(await fast.isChecked(), false);
  const fastCatalogBefore = JSON.parse(
    await readFile(path.join(codex, "uni-switch-models.json"), "utf8"),
  );
  for (const enabled of [true, false]) {
    await fast.click();
    await dismissCodexWrite();
    await until(
      async () =>
        (await overview()).providers.find((p) => p.id === gpt.id).codexOptions
          .fastMode === enabled,
    );
    await until(
      async () =>
        (await fast.isChecked()) === enabled && (await fast.isEnabled()),
    );
    assert.equal(
      (await overview()).targets.find((t) => t.target === "codex")
        .activeProviderId,
      gpt.id,
    );
    const text = await readFile(path.join(codex, "config.toml"), "utf8");
    assert.ok(
      text.includes(`service_tier = "${enabled ? "priority" : "default"}"`),
    );
    const catalog = JSON.parse(
      await readFile(path.join(codex, "uni-switch-models.json"), "utf8"),
    );
    for (const [i, model] of catalog.models.entries()) {
      assert.deepEqual(model.additional_speed_tiers, enabled ? ["fast"] : []);
      assert.equal(model.service_tiers.length, enabled ? 1 : 0);
      const unchanged = {
        ...model,
        service_tiers: fastCatalogBefore.models[i].service_tiers,
        additional_speed_tiers:
          fastCatalogBefore.models[i].additional_speed_tiers,
      };
      assert.deepEqual(unchanged, fastCatalogBefore.models[i]);
    }
    assert.equal(
      (await overview()).targets.find((t) => t.target === "codex").state,
      "applied",
    );
  }
  await audit("Supplier list Fast switch");
  await fast.click();
  await dismissCodexWrite();
  await until(async () => (await fast.isChecked()) && (await fast.isEnabled()));
  await page.screenshot({ path: "docs/screenshots/provider-fast-0.5.3.png" });
  await fast.click();
  await dismissCodexWrite();
  await until(
    async () => !(await fast.isChecked()) && (await fast.isEnabled()),
  );
  checks.push(
    "列表一键开启和关闭Fast，同时写入Codex服务档位与模型速度菜单，保持供应商和模型元数据",
  );

  const unchanged = await snapshot();
  await edit(gpt);
  await page
    .locator(".form-advanced")
    .screenshot({ path: "docs/screenshots/advanced-settings-0.5.3.png" });
  await advanced();
  await page.getByLabel("上下文长度 gpt-6.1-sol", { exact: true }).waitFor();
  await page
    .locator(".model-settings")
    .screenshot({ path: "docs/screenshots/model-settings-0.5.6.png" });
  await page
    .getByRole("checkbox", { name: "启用 gpt-5.5", exact: true })
    .check();
  await page.getByLabel("上下文长度 gpt-6.1-sol", { exact: true }).fill("512");
  await page.getByRole("radio", { name: "开启", exact: true }).check();
  assert.equal(
    await page.getByRole("button", { name: /修复.*思考强度/ }).count(),
    0,
  );
  await page
    .getByText(
      "每次写入 Codex 配置时自动检查并补齐思考强度选项，无需手动修复。",
      { exact: true },
    )
    .waitFor();
  await page.locator("#apiKey").fill("unconfirmed-key");
  await page.locator("#baseUrl").fill(base("changed-draft"));
  await page.waitForTimeout(900);
  await close();
  assert.deepEqual(
    await snapshot(),
    unchanged,
    "Cancel cannot change DB, credentials, catalog or client bytes",
  );
  const editControl = row(gpt).getByRole("button", {
    name: `编辑 ${gpt.name}`,
    exact: true,
  });
  await editControl.focus();
  await page.keyboard.press("Enter");
  await page.getByRole("dialog", { name: "编辑供应商", exact: true }).waitFor();
  await audit("Direct edit keyboard flow");
  await close();
  assert.equal(
    await editControl.evaluate((el) => el === document.activeElement),
    true,
  );
  console.log(`QA ${checks.length + 1}`);
  checks.push(
    "修改密钥、地址、模型、上下文和Fast后取消，自动修复不绕过确认，数据库及客户端文件逐字节不变；编辑入口支持键盘和焦点返回",
  );

  await edit(gpt);
  await advanced();
  await page
    .getByRole("checkbox", { name: "启用 gpt-5.5", exact: true })
    .check();
  await page.getByLabel("上下文长度 gpt-5.5", { exact: true }).fill("128");
  await page.getByLabel("上下文长度 gpt-6.1-sol", { exact: true }).fill("512");
  assert.equal(
    await page.getByRole("button", { name: /修复.*思考强度/ }).count(),
    0,
  );
  await audit("Advanced draft form");
  await page
    .getByRole("group", { name: "API 协议", exact: true })
    .scrollIntoViewIfNeeded();
  await page.evaluate(() => document.activeElement?.blur());
  await page.screenshot({
    path: "docs/screenshots/flat-advanced-form-0.5.16.png",
  });
  console.log("Resizing form");
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
    path: "docs/screenshots/ux-models-narrow.png",
  });
  await page.setViewportSize({ width: 1120, height: 780 });
  await page.getByRole("button", { name: "保存并使用", exact: true }).click();
  await finishFormWrite();
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
  await finishFormWrite();
  data = await overview();
  const claude = data.providers.find((p) => p.baseUrl === base("claude"));
  assert.deepEqual(
    claude.codexOptions.models
      .filter((m) => m.enabled)
      .map((m) => m.id)
      .sort(),
    ["claude-opus-4-6", "claude-sonnet-4-6"],
  );
  assert.equal(claude.model, "claude-sonnet-4-6");
  assert.equal(claude.authMode, "x-api-key");
  assert.equal(claude.codexOptions.upstreamProtocol, "anthropic");
  assert.equal(
    await row(claude)
      .getByRole("switch", { name: `Fast 模式 · ${claude.name}`, exact: true })
      .isDisabled(),
    true,
  );
  const inactiveConfig = await readFile(
    path.join(codex, "config.toml"),
    "utf8",
  );
  await row(gpt)
    .getByRole("switch", { name: `Fast 模式 · ${gpt.name}`, exact: true })
    .click();
  await until(
    async () =>
      (await overview()).providers.find((p) => p.id === gpt.id).codexOptions
        .fastMode === true,
  );
  assert.equal(
    await readFile(path.join(codex, "config.toml"), "utf8"),
    inactiveConfig,
  );
  assert.equal(
    (await overview()).targets.find((t) => t.target === "codex")
      .activeProviderId,
    claude.id,
  );
  await row(gpt)
    .getByRole("switch", { name: `Fast 模式 · ${gpt.name}`, exact: true })
    .click();
  await until(
    async () =>
      (await overview()).providers.find((p) => p.id === gpt.id).codexOptions
        .fastMode === false,
  );
  checks.push(
    "Claude转换的Fast显示暂不支持；未使用供应商的Fast只保存，不切换或修改当前配置",
  );
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
  const sharedFast = row(gpt).getByRole("switch", {
    name: `Fast 模式 · ${gpt.name}`,
    exact: true,
  });
  for (const enabled of [true, false]) {
    await sharedFast.click();
    await dismissCodexWrite();
    await until(
      async () =>
        (await overview()).providers.find((p) => p.id === gpt.id).codexOptions
          .fastMode === enabled && (await sharedFast.isEnabled()),
    );
    assert.ok((await overview()).targets.every((t) => t.state === "applied"));
  }
  checks.push(
    "共享供应商切换Codex的Fast后，Claude桌面与CLI继续使用各自快照且不误报待更新",
  );
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
    .getByText(/其他软件修改/)
    .waitFor();
  assert.deepEqual(await snapshot(), beforeFail);
  await writeFile(path.join(codex, "config.toml"), clean);
  await page.getByRole("button", { name: "保存并使用", exact: true }).click();
  await finishFormWrite();
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
  const syncResult = await invoke("sync_provider_targets", {
    providerId: gpt.id,
  });
  assert.ok(syncResult.length === 2 && syncResult.every((r) => r.success));
  assert.equal(
    JSON.parse(
      await readFile(
        path.join(root, "data/bridge-claude-cli-active.json"),
        "utf8",
      ),
    ).api_key,
    "edited-key-that-cannot-commit",
  );
  assert.ok((await overview()).targets.every((t) => t.state === "applied"));
  checks.push("共享密钥一键同步其他客户端，各目标返回明确结果");
  const bg = await invoke("get_background_settings");
  assert.deepEqual(bg, { supported: false, enabled: false });
  await assert.rejects(invoke("set_background_start", { enabled: true }));
  checks.push("隔离运行禁止修改Windows启动项");
  // Restore the fake upstream key through the same confirmed UI path.
  await edit(data.providers.find((p) => p.id === gpt.id));
  await page.locator("#apiKey").fill(key);
  await page.getByRole("button", { name: "保存并使用", exact: true }).click();
  await finishFormWrite();
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
  await verifyProviderLayout({ page, checks });
  await page.evaluate(() => document.activeElement?.blur());
  await page.screenshot({ path: "docs/screenshots/ux-supplier-list.png" });
  await page.screenshot({
    path: "docs/screenshots/flat-provider-list-0.5.16.png",
  });
  await page.setViewportSize({ width: 390, height: 844 });
  assert.equal(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    ),
    false,
  );
  await audit("Narrow supplier list");
  assert.equal(
    await page.locator(".provider-list select, .provider-list details").count(),
    0,
  );
  await row(gpt)
    .getByRole("button", { name: `编辑 ${gpt.name}`, exact: true })
    .click();
  await page.getByRole("dialog", { name: "编辑供应商", exact: true }).waitFor();
  await audit("Narrow flat edit form");
  assert.equal(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    ),
    false,
  );
  await close();
  await page.screenshot({ path: "docs/screenshots/ux-list-narrow.png" });
  console.log(`QA ${checks.length + 1}`);
  checks.push(
    "平铺列表不含下拉和更多菜单，390px表单/列表无横向溢出，可访问性检查无违规",
  );
  assert.deepEqual(errors, []);
  await page.keyboard.press("Escape");
  await page.setViewportSize({ width: 1120, height: 780 });
  await page.getByRole("tab", { name: /Codex/ }).click();
  const slowBefore = await snapshot();
  await add("slow");
  await page.getByRole("button", { name: "添加并使用", exact: true }).click();
  await page.getByRole("button", { name: "获取模型…", exact: true }).waitFor();
  await close();
  await page.waitForTimeout(3500);
  assert.deepEqual(await snapshot(), slowBefore);
  checks.push("原生等待模型时可以取消，迟到响应不改变数据库或客户端文件");
  hideDefault = true;
  await edit((await overview()).providers.find((p) => p.id === gpt.id));
  await page.getByLabel("启用 gpt-5.5", { exact: true }).waitFor();
  await page.getByLabel("启用 gpt-6.1-sol", { exact: true }).waitFor({ state: "hidden" });
  assert.equal(await page.getByText("最新列表未包含", { exact: true }).count(), 0);
  await close();
  assert.deepEqual(await snapshot(), slowBefore);
  hideDefault = false;
  checks.push("默认模型下架后同步只显示最新模型并自动选择可用默认值，取消草稿不改供应商和客户端文件");
  const existing = (await overview()).providers.find((p) => p.id === gpt.id);
  const duplicate = await invoke("commit_provider", {
    target: "codex",
    apply: true,
    input: {
      id: null,
      family: existing.family,
      name: existing.name,
      baseUrl: existing.baseUrl,
      apiKey: key,
      model: existing.model,
      authMode: existing.authMode,
      reasoningEffort: existing.reasoningEffort,
      codexOptions: existing.codexOptions,
    },
  });
  assert.equal(duplicate.id, gpt.id);
  assert.equal(duplicate.reusedExisting, true);
  assert.equal((await overview()).providers.length, 2);
  checks.push("同地址密钥协议及模型设置完全相同的供应商复用ID，不重复添加");
  await page.getByRole("button", { name: "设置", exact: true }).click();
  assert.equal(
    await page
      .getByRole("button", { name: "修改目录", exact: true })
      .isEnabled(),
    false,
  );
  await audit("Background and supplier settings");
  await page.getByRole("button", { name: "关闭弹窗", exact: true }).click();
  for (const zoom of [1.25, 1.5, 2]) {
    await page.setViewportSize({
      width: Math.floor(1120 / zoom),
      height: Math.floor(780 / zoom),
    });
    assert.equal(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
      false,
    );
    await page.getByRole("button", { name: "添加供应商", exact: true }).click();
    const dialog = page.getByRole("dialog");
    assert.equal(
      await dialog.evaluate((el) => el.scrollWidth > el.clientWidth),
      false,
    );
    assert.ok(
      await page
        .getByRole("button", { name: "添加并使用", exact: true })
        .isVisible(),
    );
    await close();
  }
  checks.push("等效125%、150%、200%缩放下列表与添加表单无横向溢出，主按钮可见");
  await page.setViewportSize({ width: 1120, height: 780 });
  const quickBefore = await snapshot();
  const quickProvider = (await overview()).providers.find(
    (p) => p.id === gpt.id,
  );
  await row(quickProvider)
    .getByRole("button", {
      name: `配置 ${quickProvider.name} 的模型`,
      exact: true,
    })
    .click();
  assert.equal(await page.locator(".model-config-dialog").isVisible(), true);
  assert.equal(await page.getByRole("dialog").count(), 1);
  assert.equal(
    await page.getByRole("group", { name: "API 协议", exact: true }).count(),
    0,
  );
  assert.equal(
    await page
      .locator(".model-config-dialog .model-selection")
      .evaluate((el) => el === document.activeElement),
    true,
  );
  await page.getByRole("button", { name: "关闭弹窗", exact: true }).click();
  assert.deepEqual(await snapshot(), quickBefore);
  checks.push("模型配置在独立弹窗展示，取消不写入，未进入完整编辑界面");
  await verifyInlineModels({
    page,
    overview,
    invoke,
    snapshot,
    row,
    gpt,
    checks,
    audit,
  });
  await page.keyboard.press("Control+f");
  const supplierSearch = page.getByRole("textbox", {
    name: "搜索 API 配置",
    exact: true,
  });
  await supplierSearch.fill("no-match");
  await page.getByText("没有找到匹配的配置", { exact: true }).waitFor();
  await page.keyboard.press("Escape");
  assert.equal(await supplierSearch.inputValue(), "");
  await row(quickProvider).waitFor();
  checks.push("少量供应商Ctrl+F可搜索，Escape清空并保留列表");
  await add("many");
  await page.locator("#baseUrl").fill(base("many") + "/chat/completions");
  await page.locator("#apiKey").fill(` \"Bearer ${key}\" `);
  await page.locator("#apiKey").blur();
  await page
    .getByRole("button", { name: "显示更多模型", exact: true })
    .waitFor();
  assert.equal(await page.locator("#baseUrl").inputValue(), base("many"));
  assert.equal(await page.locator("#apiKey").inputValue(), key);
  assert.equal(
    await page.getByRole("checkbox", { name: /^启用 / }).count(),
    40,
  );
  await page
    .getByRole("textbox", { name: "搜索模型", exact: true })
    .fill("gpt-0");
  await page
    .getByRole("button", { name: "将 gpt-0 设为默认模型", exact: true })
    .click();
  await page.getByRole("button", { name: /只看已启用/ }).click();
  assert.equal(
    await page.getByRole("checkbox", { name: /^启用 / }).count(),
    40,
  );
  assert.equal(
    await page
      .getByRole("checkbox", { name: /^启用 / })
      .evaluateAll((controls) => controls.every((control) => control.checked)),
    true,
  );
  await audit("Large model list and selection filters");
  await page.setViewportSize({ width: 390, height: 760 });
  const largeDialog = page.getByRole("dialog");
  assert.equal(
    await largeDialog.evaluate((el) => el.scrollWidth > el.clientWidth),
    false,
  );
  await page.screenshot({ path: "docs/screenshots/ux-models-0.5.1.png" });
  await page.setViewportSize({ width: 1120, height: 780 });
  await page
    .locator(".modal-overlay")
    .click({ position: { x: 3, y: 3 }, force: true });
  assert.equal(await page.getByRole("dialog").isVisible(), true);
  await page.getByRole("button", { name: "添加并使用", exact: true }).click();
  await finishFormWrite();
  const manyProvider = (await overview()).providers.find(
    (p) => p.baseUrl === base("many"),
  );
  assert.equal(manyProvider.model, "gpt-0");
  assert.equal(
    manyProvider.codexOptions.models.filter((m) => m.enabled).length,
    85,
  );
  const manyCatalog = JSON.parse(
    await readFile(path.join(codex, "uni-switch-models.json"), "utf8"),
  );
  assert.equal(manyCatalog.models.length, 85);
  assert.deepEqual(
    manyCatalog.models.map((m) => m.slug).sort(),
    manyProvider.codexOptions.models.map((m) => m.id).sort(),
  );
  assert.ok(
    manyProvider.codexOptions.models
      .filter((m) => m.enabled)
      .every((m) => m.contextWindow === 256000),
  );
  assert.ok(
    requests
      .filter((r) => r.site === "many" && r.path.endsWith("/models"))
      .every((r) => r.bearer),
  );
  checks.push(
    "85个上游模型默认全选，分页和搜索不丢失选择，全部模型与256k上下文确认后写入Codex目录",
  );
  checks.push("完整请求地址与Bearer密钥自动整理，点击弹窗外侧保留表单");
  await row(manyProvider)
    .getByRole("button", { name: `置顶 ${manyProvider.name}`, exact: true })
    .click();
  await row(manyProvider)
    .getByRole("button", { name: `取消置顶 ${manyProvider.name}`, exact: true })
    .waitFor();
  await page.keyboard.press("Escape");
  await audit("Supplier model shortcut and pin state");
  await page.screenshot({ path: "docs/screenshots/ux-list-0.5.1.png" });
  checks.push("置顶状态在行内可见，模型快捷入口可访问性检查通过");
  await mkdir(path.dirname(fixtureImage), { recursive: true });
  await copyFile(
    path.resolve("src-tauri/target/debug/examples/codex_restart_fixture.exe"),
    fixtureImage,
  );
  fixture = spawn(fixtureImage, [`--user-data-dir=${fixtureUserData}`], {
    windowsHide: true,
    stdio: "ignore",
    env: { ...process.env, CODEX_HOME: codex },
  });
  await until(
    async () =>
      (await invoke("get_runtime_status", { target: "codex" })).desktopRunning,
  );
  const initialFixture = JSON.parse(await readFile(fixtureMarker, "utf8"));
  fixturePid = initialFixture.pid;
  const currentFast = row(manyProvider).getByRole("switch", {
    name: `Fast 模式 · ${manyProvider.name}`,
    exact: true,
  });
  await currentFast.click();
  const restartDialog = page.getByRole("dialog", {
    name: "重启 Codex 使配置生效",
    exact: true,
  });
  await restartDialog.waitFor();
  assert.equal(
    await page
      .getByRole("button", { name: "稍后重启", exact: true })
      .evaluate((el) => el === document.activeElement),
    true,
  );
  await audit("Codex restart dialog");
  await page.screenshot({ path: "docs/screenshots/codex-restart-0.5.6.png" });
  await page.getByRole("button", { name: "稍后重启", exact: true }).click();
  await page.waitForTimeout(11000);
  assert.equal(await restartDialog.count(), 0);
  assert.equal(fixture.exitCode, null);
  checks.push(
    "配置写入后弹重启选择，稍后保留Codex运行，状态轮询不重复弹同次修改",
  );
  await currentFast.click();
  await restartDialog.waitFor();
  const revision = (await invoke("get_runtime_status", { target: "codex" }))
    .configurationRevision;
  const stale = await page.evaluate(async (revision) => {
    try {
      return await window.__TAURI_INTERNALS__.invoke("restart_codex_desktop", {
        configurationRevision: revision - 1,
      });
    } catch (error) {
      return error;
    }
  }, revision);
  assert.equal(stale.code, "restart_configuration_changed");
  assert.equal(fixture.exitCode, null);
  const beforeRestart = await snapshot();
  await page.getByRole("button", { name: "立即重启", exact: true }).click();
  await restartDialog.waitFor({ state: "hidden", timeout: 65000 });
  await until(
    async () =>
      !(await invoke("get_runtime_status", { target: "codex" }))
        .desktopRestartRequired,
  );
  const newFixture = JSON.parse(await readFile(fixtureMarker, "utf8"));
  assert.notEqual(newFixture.pid, initialFixture.pid);
  assert.equal(newFixture.codexHome, codex);
  assert.deepEqual(newFixture.args, [`--user-data-dir=${fixtureUserData}`]);
  fixturePid = newFixture.pid;
  assert.deepEqual(await snapshot(), beforeRestart);
  checks.push(
    "立即重启正常关闭并重新打开隔离桌面进程，保留配置与中文用户数据目录；旧配置版本禁止误重启",
  );
  await killFixture();
  fixture = spawn(fixtureImage, [`--user-data-dir=${fixtureUserData}`], {
    windowsHide: true,
    stdio: "ignore",
    env: {
      ...process.env,
      CODEX_HOME: codex,
      UNI_SWITCH_RESTART_IGNORE_CLOSE: "1",
    },
  });
  await until(
    async () =>
      (await invoke("get_runtime_status", { target: "codex" })).desktopRunning,
  );
  await currentFast.click();
  await restartDialog.waitFor();
  await page.getByRole("button", { name: "立即重启", exact: true }).click();
  await restartDialog
    .getByRole("alert")
    .getByText(/Codex 尚未退出/)
    .waitFor({ timeout: 65000 });
  assert.equal(fixture.exitCode, null);
  assert.equal(
    await page
      .getByRole("button", { name: "稍后重启", exact: true })
      .isEnabled(),
    true,
  );
  await page.getByRole("button", { name: "稍后重启", exact: true }).click();
  await killFixture();
  checks.push("Codex拒绝正常退出时重启失败保留提示与稍后选项，不强制结束进程");
  await page.getByRole("button", { name: "设置", exact: true }).click();
  await page.getByRole("button", { name: "恢复原配置", exact: true }).click();
  fixture = spawn(fixtureImage, [`--user-data-dir=${fixtureUserData}`], {
    windowsHide: true,
    stdio: "ignore",
    env: { ...process.env, CODEX_HOME: codex },
  });
  await until(
    async () =>
      (await invoke("get_runtime_status", { target: "codex" })).desktopRunning,
  );
  await page.getByRole("button", { name: "恢复原配置", exact: true }).click();
  await restartDialog.waitFor();
  await page.getByRole("button", { name: "稍后重启", exact: true }).click();
  await killFixture();
  checks.push("恢复原配置也提示重启，未接管状态仍记录真实配置写入");

  console.log("QA Claude desktop restart");
  const claudeFixtureImage = path.join(
    root,
    "WindowsApps",
    "Claude_qa",
    "app",
    "Claude.exe",
  );
  const claudeUserData = path.join(local, "Claude-3p");
  const claudeMarker = path.join(claudeUserData, "restart-fixture.json");
  await mkdir(path.dirname(claudeFixtureImage), { recursive: true });
  await copyFile(
    path.resolve("src-tauri/target/debug/examples/codex_restart_fixture.exe"),
    claudeFixtureImage,
  );
  await invoke("apply_provider", {
    target: "claude_desktop",
    providerId: claude.id,
  });
  currentFixtureImage = claudeFixtureImage;
  fixture = spawn(claudeFixtureImage, [`--user-data-dir=${claudeUserData}`], {
    windowsHide: true,
    stdio: "ignore",
    env: { ...process.env, LOCALAPPDATA: local },
  });
  await until(
    async () =>
      (await invoke("get_runtime_status", { target: "claude_desktop" }))
        .desktopRunning,
  );
  const oldClaude = JSON.parse(await readFile(claudeMarker, "utf8"));
  fixturePid = oldClaude.pid;
  await page.getByRole("tab", { name: /Claude Code/ }).click();
  await page.getByRole("button", { name: "桌面端", exact: true }).click();
  await invoke("apply_provider", {
    target: "claude_desktop",
    providerId: manyProvider.id,
  });
  await page.getByRole("button", { name: "刷新配置状态", exact: true }).click();
  const claudeRestartDialog = page.getByRole("dialog", {
    name: "重启 Claude Code 桌面端 使配置生效",
    exact: true,
  });
  await claudeRestartDialog.waitFor();
  await audit("Claude desktop restart dialog");
  await page.screenshot({ path: "docs/screenshots/claude-restart-0.5.6.png" });
  await page.getByRole("button", { name: "稍后重启", exact: true }).click();
  await page.waitForTimeout(11000);
  assert.equal(await claudeRestartDialog.count(), 0);
  assert.equal(fixture.exitCode, null);
  await row(manyProvider)
    .getByRole("button", { name: "重启 Claude Code 桌面端", exact: true })
    .click();
  const claudeRevision = (
    await invoke("get_runtime_status", { target: "claude_desktop" })
  ).configurationRevision;
  await assert.rejects(
    invoke("restart_client", {
      target: "claude_desktop",
      configurationRevision: claudeRevision - 1,
    }),
    (error) => error.code === "restart_configuration_changed",
  );
  const beforeClaudeRestart = await snapshot();
  await page.getByRole("button", { name: "立即重启", exact: true }).click();
  await claudeRestartDialog.waitFor({ state: "hidden", timeout: 65000 });
  const newClaude = JSON.parse(await readFile(claudeMarker, "utf8"));
  fixturePid = newClaude.pid;
  assert.notEqual(newClaude.pid, oldClaude.pid);
  assert.equal(newClaude.localAppData, local);
  assert.deepEqual(newClaude.args, [`--user-data-dir=${claudeUserData}`]);
  assert.equal(
    (await invoke("get_runtime_status", { target: "claude_desktop" }))
      .desktopRestartRequired,
    false,
  );
  assert.deepEqual(await snapshot(), beforeClaudeRestart);
  checks.push(
    "Claude桌面端真实写入后弹窗，稍后不重复；立即正常退出重开并保留用户目录，旧版本拒绝且配置不变",
  );
  await killFixture();

  console.log("QA Claude CLI assisted restart");
  await mkdir(path.dirname(cliFixtureImage), { recursive: true });
  await mkdir(cliWorkingDirectory, { recursive: true });
  await copyFile(
    path.resolve(
      "src-tauri/target/debug/examples/claude_cli_restart_fixture.exe",
    ),
    cliFixtureImage,
  );
  await invoke("apply_provider", {
    target: "claude_cli",
    providerId: claude.id,
  });
  const cliFixture = spawn(cliFixtureImage, ["--model", "old-model"], {
    windowsHide: true,
    stdio: "ignore",
    cwd: cliWorkingDirectory,
    env: {
      ...process.env,
      CLAUDE_CONFIG_DIR: cli,
      UNI_SWITCH_CLI_FIXTURE_MARKER: cliFixtureMarker,
    },
  });
  cliFixturePids.add(cliFixture.pid);
  await until(
    async () =>
      (await invoke("get_runtime_status", { target: "claude_cli" }))
        .clientRunning,
  );
  const oldCli = JSON.parse(await readFile(cliFixtureMarker, "utf8"));
  await page.getByRole("button", { name: "CLI", exact: true }).click();
  await invoke("apply_provider", {
    target: "claude_cli",
    providerId: manyProvider.id,
  });
  await page.getByRole("button", { name: "刷新配置状态", exact: true }).click();
  const cliRestartDialog = page.getByRole("dialog", {
    name: "重启 Claude CLI 使配置生效",
    exact: true,
  });
  await cliRestartDialog.waitFor();
  assert.equal(
    await cliRestartDialog
      .getByText("claude --continue", { exact: true })
      .count(),
    1,
  );
  await audit("Claude CLI restart dialog");
  await page.screenshot({
    path: "docs/screenshots/claude-cli-restart-0.5.6.png",
  });
  await page.getByRole("button", { name: "稍后重启", exact: true }).click();
  await page.waitForTimeout(11000);
  assert.equal(await cliRestartDialog.count(), 0);
  assert.equal(cliFixture.exitCode, null);
  checks.push(
    "Claude CLI真实写入后弹同样选择，说明退出及恢复会话；稍后保持进程且不反复提示",
  );
  await row(manyProvider)
    .getByRole("button", { name: "重启 Claude CLI", exact: true })
    .click();
  const cliRevision = (
    await invoke("get_runtime_status", { target: "claude_cli" })
  ).configurationRevision;
  await assert.rejects(
    invoke("restart_client", {
      target: "claude_cli",
      configurationRevision: cliRevision - 1,
    }),
    (error) => error.code === "restart_configuration_changed",
  );
  const beforeCliRestart = await snapshot();
  await page.getByRole("button", { name: "立即重启", exact: true }).click();
  await cliRestartDialog.waitFor({ state: "hidden" });
  await until(
    async () =>
      (await invoke("get_runtime_status", { target: "claude_cli" }))
        .restartInProgress,
  );
  assert.equal(
    cliFixture.exitCode,
    null,
    "old terminal is preserved until user exits",
  );
  const duplicateCliRestart = await invoke("restart_client", {
    target: "claude_cli",
    configurationRevision: cliRevision,
  });
  assert.equal(duplicateCliRestart.restarted, false);
  assert.equal(duplicateCliRestart.pending, true);
  assert.equal(
    JSON.parse(await readFile(cliFixtureMarker, "utf8")).pid,
    oldCli.pid,
  );
  await writeFile(
    cliFixtureMarker.replace(/\.json$/, `.exit-${oldCli.pid}`),
    "normal exit",
  );
  await until(
    async () =>
      JSON.parse(await readFile(cliFixtureMarker, "utf8")).pid !== oldCli.pid,
  );
  const newCli = JSON.parse(await readFile(cliFixtureMarker, "utf8"));
  cliFixturePids.add(newCli.pid);
  assert.equal(newCli.configDirectory, cli);
  assert.equal(newCli.currentDirectory, cliWorkingDirectory);
  assert.deepEqual(newCli.args, ["--continue"]);
  assert.equal(newCli.inferenceEnvironmentPresent, false);
  assert.equal(
    (await invoke("get_runtime_status", { target: "claude_cli" }))
      .restartRequired,
    false,
  );
  assert.deepEqual(await snapshot(), beforeCliRestart);
  checks.push(
    "CLI立即重启打开接续终端并等旧进程正常退出，保留中文工作目录和配置目录、不重放旧模型参数，重复点击去重且旧版本拒绝",
  );
  await stopCliFixtures();
  assert.deepEqual(errors, []);
  assert.deepEqual(errors, []);
  const result = {
    version: "0.5.16",
    passed: checks.length,
    skippedChecks,
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
    path.resolve(".qa/ux-flow/results.json"),
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
  await stopCliFixtures();
  await killFixture();
  if (browser || child) await stop();
  if (occupied) await new Promise((r) => occupied.close(r));
  await new Promise((r) => server.close(r));
}
