import { chromium } from "playwright";
import AxeBuilder from "@axe-core/playwright";
import { spawn } from "node:child_process";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import http from "node:http";
import path from "node:path";
import assert from "node:assert/strict";
import { verifyDesktopReasoning } from "./qa-codex-desktop-reasoning.mjs";

const root = path.resolve(".qa/reasoning-repair");
const run = path.join(root, "native", Date.now().toString());
const codexHome = path.join(run, "Codex 中文");
await mkdir(codexHome, { recursive: true });
await mkdir("docs/screenshots", { recursive: true });
const configPath = path.join(codexHome, "config.toml");
const catalogPath = path.join(codexHome, "uni-switch-models.json");
await writeFile(
  configPath,
  '# original\nmodel = "original"\nmodel_reasoning_effort = "medium"\n\n[desktop]\nenabled-reasoning-efforts = ["low", "medium", "high", "xhigh"]\ntheme = "dark"\n',
);
const key = "isolated-repair-key";
const ids = ["gateway-main", "gateway-mini"];
const checks = [],
  accessibility = [],
  errors = [];
const server = http.createServer((req, res) => {
  const send = (body, code = 200) => {
    res.writeHead(code, { "Content-Type": "application/json" });
    res.end(JSON.stringify(body));
  };
  if (req.headers.authorization !== `Bearer ${key}`) return send({}, 401);
  if (req.url === "/v1/models")
    return send({
      data: ids.map((id) => ({
        id,
        context_window: 128000,
        reasoning_efforts: ["low"],
      })),
    });
  if (req.url === "/v1/usage")
    return send({ mode: "unrestricted", balance: 12.5, unit: "USD" });
  send({}, 404);
});
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const baseUrl = `http://127.0.0.1:${server.address().port}/v1`;
const codexBinary =
  process.env.CODEX_QA_BIN ||
  "C:/Users/Administrator/AppData/Local/OpenAI/Codex/bin/8aaf1547b825b104/codex.exe";
const configReads = new Map();
async function modelList(label) {
  const env = { ...process.env, CODEX_HOME: codexHome };
  delete env.OPENAI_API_KEY;
  const proc = spawn(codexBinary, ["app-server"], {
    cwd: codexHome,
    env,
    windowsHide: true,
  });
  let buffer = "",
    stderr = "",
    resolveList,
    rejectList,
    modelResult,
    configResult;
  const ready = new Promise((resolve, reject) => {
    resolveList = resolve;
    rejectList = reject;
  });
  proc.on("error", rejectList);
  proc.stderr.on("data", (c) => {
    stderr += c;
  });
  proc.on("exit", (code) =>
    rejectList(Error(`Codex exited ${code}: ${stderr}`)),
  );
  proc.stdout.on("data", (chunk) => {
    buffer += chunk;
    const lines = buffer.split("\n");
    buffer = lines.pop();
    for (const line of lines) {
      let message;
      try {
        message = JSON.parse(line);
      } catch {
        continue;
      }
      if (message.error) {
        rejectList(Error(JSON.stringify(message.error)));
        continue;
      }
      if (message.id === 1) {
        proc.stdin.write(JSON.stringify({ method: "initialized" }) + "\n");
        proc.stdin.write(
          JSON.stringify({
            id: 2,
            method: "model/list",
            params: { limit: 100, includeHidden: false },
          }) + "\n",
        );
        proc.stdin.write(
          JSON.stringify({
            id: 3,
            method: "config/read",
            params: { includeLayers: false },
          }) + "\n",
        );
      }
      if (message.id === 2) modelResult = message.result;
      if (message.id === 3) configResult = message.result;
      if (modelResult && configResult)
        resolveList({ models: modelResult, config: configResult });
    }
  });
  proc.stdin.write(
    JSON.stringify({
      id: 1,
      method: "initialize",
      params: {
        clientInfo: {
          name: "uni_switch_qa",
          title: "uni-switch QA",
          version: "0.3.6",
        },
        capabilities: { experimentalApi: true },
      },
    }) + "\n",
  );
  const timeout = setTimeout(() => {
    rejectList(Error(`model/list timed out: ${stderr}`));
    proc.kill();
  }, 30000);
  try {
    const result = await ready;
    await writeFile(
      path.join(root, `codex-model-list-${label}.json`),
      JSON.stringify(result.models, null, 2),
    );
    await writeFile(
      path.join(root, `codex-config-read-${label}.json`),
      JSON.stringify(result.config, null, 2),
    );
    configReads.set(label, result.config.config);
    return result.models.data;
  } finally {
    clearTimeout(timeout);
    proc.kill();
  }
}
const until = async (fn) => {
  const end = Date.now() + 25000;
  while (Date.now() < end) {
    if (await fn()) return;
    await new Promise((r) => setTimeout(r, 150));
  }
  throw Error("Native QA timed out");
};
let child, browser, page;
try {
  let occupied = false;
  try {
    occupied = (await fetch("http://127.0.0.1:9223/json/version")).ok;
  } catch {}
  assert.equal(occupied, false, "Another process owns QA port");
  child = spawn(
    path.resolve(
      process.env.UNI_SWITCH_QA_EXE ||
        ".qa/reasoning-repair/test-app/uni-switch.exe",
    ),
    [],
    {
      windowsHide: true,
      stdio: "ignore",
      env: {
        ...process.env,
        UNI_SWITCH_DATA_DIR: path.join(run, "data"),
        WEBVIEW2_USER_DATA_FOLDER: path.join(run, "webview"),
        CODEX_HOME: codexHome,
        CLAUDE_CONFIG_DIR: path.join(run, "claude-cli"),
        LOCALAPPDATA: path.join(run, "local-app-data"),
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
  page.setDefaultTimeout(15000);
  page.on("pageerror", (e) => errors.push(e.message));
  await page.getByRole("button", { name: /^添加(第一组)?配置$/ }).waitFor();
  const invoke = (name, args = {}) =>
    page.evaluate(
      ({ name, args }) => window.__TAURI_INTERNALS__.invoke(name, args),
      { name, args },
    );
  const input = {
    id: null,
    family: "codex",
    name: "思考强度测试",
    baseUrl,
    apiKey: key,
    balanceAccessToken: null,
    model: ids[0],
    authMode: "bearer",
    reasoningEffort: "high",
    codexOptions: {
      fastMode: true,
      contextWindow: 128000,
      autoCompactTokenLimit: 100000,
      models: ids.map((id) => ({
        id,
        contextWindow: 128000,
        reasoningEfforts: ["low"],
        enabled: true,
      })),
      modelsSyncedAt: 1,
      balanceQuery: {
        adapter: "auto",
        path: "",
        jsonPath: "",
        unit: "USD",
        divisor: 1,
      },
    },
  };
  const provider = await invoke("save_provider", { input });
  await invoke("apply_provider", { target: "codex", providerId: provider.id });
  const beforeModels = await modelList("before");
  assert.ok(
    beforeModels.every((m) =>
      m.supportedReasoningEfforts.every((e) => e.reasoningEffort !== "max"),
    ),
  );
  checks.push("真实 Codex 修复前只列出供应商提供的 low，复现缺少 max");

  const text = (await readFile(configPath, "utf8")).replace(
    'model = "gateway-main"',
    'model = "gateway-mini"',
  );
  await writeFile(configPath, text + "\n[extra]\nkeep = true\n");
  const beforeConfig = await readFile(configPath, "utf8");
  await page.reload();
  await page
    .getByRole("button", { name: "编辑 思考强度测试", exact: true })
    .click();
  await page
    .getByRole("checkbox", { name: "启用 gateway-main", exact: true })
    .waitFor();
  await page.locator(".form-advanced > summary").click();
  assert.equal(await page.locator("#reasoningEffort").count(), 0);
  const repair = page.getByRole("button", {
    name: "修复思考强度列表",
    exact: true,
  });
  await repair.focus();
  await page.keyboard.press("Enter");
  await page.getByText(/已修复当前已应用配置/).waitFor();
  const normalizeDisplay = (text) =>
    text.replace(
      /^enabled-reasoning-efforts\s*=.*$/m,
      "enabled-reasoning-efforts = <display setting>",
    );
  assert.equal(
    normalizeDisplay(await readFile(configPath, "utf8")),
    normalizeDisplay(beforeConfig),
  );
  let catalog = JSON.parse(await readFile(catalogPath, "utf8"));
  assert.ok(
    catalog.models.every(
      (m) =>
        m.default_reasoning_level === "low" &&
        m.context_window === 128000 &&
        m.supported_reasoning_levels.some((e) => e.effort === "max"),
    ),
  );
  let overview = await invoke("get_overview");
  assert.equal(
    overview.targets.find((t) => t.target === "codex").state,
    "applied",
  );
  const once = await readFile(catalogPath, "utf8");
  await repair.click();
  await page.getByText(/已修复当前已应用配置/).waitFor();
  assert.equal(await readFile(catalogPath, "utf8"), once);
  checks.push(
    "原生按钮及 Enter 键立即修复，重复点击无重复项，当前模型/强度/Fast/上下文/凭据完全保留",
  );

  const afterModels = await modelList("after");
  assert.deepEqual(afterModels.map((m) => m.id).sort(), ids.toSorted());
  for (const m of afterModels)
    assert.deepEqual(
      m.supportedReasoningEfforts.map((e) => e.reasoningEffort).toSorted(),
      [
        "none",
        "minimal",
        "low",
        "medium",
        "high",
        "xhigh",
        "max",
        "ultra",
      ].toSorted(),
    );
  checks.push(
    "真实 Codex model/list 已输出两模型全部八项思考强度，包括 max 和 ultra",
  );
  await verifyDesktopReasoning({
    models: afterModels,
    config: configReads.get("after"),
    output: path.join(root, "desktop-menu-verification.json"),
  });
  checks.push(
    "执行当前安装的 Codex 桌面端菜单过滤函数：仅修模型目录时 max 仍隐藏，补齐实际显示设置后 max 保留；ultra 仍遵守功能开关",
  );

  const audit = async (label) => {
    // Measure the displayed state after the dialog's short entrance animation.
    await page.evaluate(() =>
      Promise.all(
        document
          .getAnimations()
          .filter((animation) =>
            Number.isFinite(animation.effect?.getTiming().iterations),
          )
          .map((animation) => animation.finished.catch(() => {})),
      ),
    );
    const result = await new AxeBuilder({ page }).analyze();
    accessibility.push({
      label,
      violations: result.violations.map((v) => v.id),
    });
    assert.deepEqual(result.violations, [], label);
  };
  await audit("修复成功桌面弹窗");
  await page
    .locator(".reasoning-repair")
    .screenshot({ path: "docs/screenshots/reasoning-repair-section.png" });
  await page.screenshot({ path: "docs/screenshots/reasoning-repair-form.png" });
  await page.setViewportSize({ width: 760, height: 600 });
  await repair.scrollIntoViewIfNeeded();
  await audit("760 小窗口");
  await page.setViewportSize({ width: 390, height: 844 });
  await repair.scrollIntoViewIfNeeded();
  await audit("390 窄窗口");
  assert.ok(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  );
  await page.setViewportSize({ width: 1120, height: 780 });
  await page.getByRole("button", { name: "刷新模型列表", exact: true }).click();
  await until(() =>
    page.getByRole("button", { name: "刷新模型列表", exact: true }).isEnabled(),
  );
  await page.getByRole("button", { name: "保存配置", exact: true }).click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  await invoke("apply_provider", { target: "codex", providerId: provider.id });
  catalog = JSON.parse(await readFile(catalogPath, "utf8"));
  assert.ok(
    catalog.models.every((m) =>
      m.supported_reasoning_levels.some((e) => e.effort === "max"),
    ),
  );
  checks.push(
    "刷新模型及保存重新应用后修复持续保留；三种尺寸 Axe 无问题且窄窗口无横向溢出",
  );

  const inactive = await invoke("save_provider", {
    input: { ...input, name: "未应用供应商" },
  });
  const configNow = await readFile(configPath, "utf8"),
    catalogNow = await readFile(catalogPath, "utf8");
  const inactiveRepair = await invoke("repair_reasoning_levels", {
    providerId: inactive.id,
  });
  assert.equal(inactiveRepair.applied, false);
  assert.equal(await readFile(configPath, "utf8"), configNow);
  assert.equal(await readFile(catalogPath, "utf8"), catalogNow);
  await page.reload();
  await page
    .getByRole("button", { name: "编辑 未应用供应商", exact: true })
    .click();
  await page.locator(".form-advanced > summary").click();
  await repair.click();
  await page.getByText(/已为此供应商保存修复设置/).waitFor();
  await page.getByRole("button", { name: "取消", exact: true }).click();
  checks.push("未应用供应商保存待生效修复，不切换供应商、不改正在使用的配置");

  await page.getByRole("button", { name: /^添加(第一组)?配置$/ }).click();
  await page.locator(".form-advanced > summary").click();
  await repair.click();
  await page.getByText(/已准备修复/).waitFor();
  await page.getByRole("button", { name: "取消", exact: true }).click();
  const changed = JSON.parse(catalogNow);
  changed.models[0].description = "external change";
  await writeFile(catalogPath, JSON.stringify(changed));
  await page
    .getByRole("button", { name: "编辑 思考强度测试", exact: true })
    .click();
  await page.locator(".form-advanced > summary").click();
  await repair.click();
  await page
    .getByRole("alert")
    .filter({ hasText: /已被其他工具修改/ })
    .waitFor();
  assert.equal(await readFile(catalogPath, "utf8"), JSON.stringify(changed));
  await audit("外部修改错误");
  await page.getByRole("button", { name: "取消", exact: true }).click();
  await writeFile(catalogPath, catalogNow);
  await invoke("restore_original", { target: "codex" });
  const restored = await readFile(configPath, "utf8");
  assert.ok(
    restored.includes('model_reasoning_effort = "medium"') &&
      restored.includes("keep = true") &&
      restored.includes(
        'enabled-reasoning-efforts = ["low", "medium", "high", "xhigh"]',
      ) &&
      restored.includes('theme = "dark"') &&
      !restored.includes("model_catalog_json"),
  );
  checks.push(
    "新供应商提示待应用；外部模型目录修改拒绝覆盖并显示原因；恢复原配置保留无关设置",
  );
  assert.deepEqual(errors, []);
  const result = { version: "0.3.6", run, checks, accessibility, errors };
  await writeFile(
    path.join(root, "results.json"),
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
  if (child) child.kill();
  await new Promise((resolve) => server.close(resolve));
}
