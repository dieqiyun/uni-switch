import assert from "node:assert/strict";
import http from "node:http";
import path from "node:path";
import { spawn, execFileSync } from "node:child_process";
import { mkdir, readFile, writeFile, copyFile } from "node:fs/promises";
import { chromium } from "playwright";
import AxeBuilder from "@axe-core/playwright";
import { verifyTutorial } from "./qa-tutorial.mjs";
import { verifyWorkspaceHeader } from "./qa-workspace-header.mjs";
import { verifyAppUpdates } from "./qa-app-update.mjs";
import { verifyBrandRefresh } from "./qa-brand-refresh.mjs";
import {
  verifyCodexModels,
  mockModelResponse,
} from "./qa-codex-model-runtime.mjs";

const version = JSON.parse(await readFile("package.json", "utf8")).version;
const root = path.resolve(".qa/model-dialog", String(Date.now()));
const home = path.join(root, "用户 中文");
const codex = path.join(home, ".codex");
const local = path.join(root, "Local");
const cli = path.join(home, ".claude");
await Promise.all(
  [codex, local, cli, "docs/screenshots"].map((p) =>
    mkdir(p, { recursive: true }),
  ),
);
await writeFile(
  path.join(codex, "config.toml"),
  "# isolated original\nmodel='original'\n",
);
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
const key = "isolated-model-dialog-key";
const modelIds = [
  "gpt-6.1-sol",
  "codex-auto-review",
  "gpt-5.6-sol",
  "gpt-5.6-terra",
  "gpt-6-sol",
];
let many = false;
let upstreamIds = null;
let modelFailure = false;
const inferenceRequests = [];
const updateMock = { mode: "new", requests: [] };
const server = http.createServer((req, res) => {
  if (req.method === "POST" && req.url.endsWith("/responses"))
    return void mockModelResponse(req, res, inferenceRequests);
  const send = (value, code = 200) => {
    res.writeHead(code, { "Content-Type": "application/json" });
    res.end(JSON.stringify(value));
  };
  if (req.url === "/github/releases/latest") {
    updateMock.requests.push({
      auth: !!req.headers.authorization,
      key: !!req.headers["x-api-key"],
    });
    if (updateMock.mode === "rate") return send({}, 429);
    if (updateMock.mode === "missing") return send({}, 404);
    const tag =
      updateMock.mode === "current"
        ? `v${version}`
        : updateMock.mode === "old"
          ? "v0.1.0"
          : "v9.0.0";
    return send({
      tag_name: tag,
      html_url: `https://github.com/example/uni-switch/releases/tag/${tag}`,
      draft: false,
      prerelease: false,
      body: "更新模型配置体验。\n新增 GitHub 版本检测。",
      assets: [],
      published_at: "2026-10-07T00:00:00Z",
    });
  }
  if (req.url.endsWith("/models")) {
    if (req.headers.authorization !== `Bearer ${key}`) return send({}, 401);
    if (modelFailure)
      return send({ error: "isolated model endpoint unavailable" }, 503);
    return send({
      object: "list",
      data: (
        upstreamIds ??
        (many
          ? [
              ...modelIds,
              ...Array.from({ length: 80 }, (_, i) => `gpt-extra-${i}`),
            ]
          : modelIds)
      ).map((id) => ({ id, object: "model" })),
    });
  }
  if (req.url.endsWith("/usage"))
    return send({ mode: "unrestricted", balance: 7.83, unit: "USD" });
  send({}, 404);
});
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const baseUrl = `http://127.0.0.1:${server.address().port}/v1`;
let app, browser, page, appEnv;
const errors = [],
  checks = [],
  audits = [];
async function until(fn) {
  const end = Date.now() + 20000;
  while (Date.now() < end) {
    if (await fn()) return;
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error("Model dialog QA timeout");
}
const portOpen = async () => {
  try {
    return (await fetch("http://127.0.0.1:9223/json/version")).ok;
  } catch {
    return false;
  }
};
async function invoke(name, args = {}) {
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
}
const overview = () => invoke("get_overview");
const snapshot = async () => ({
  providers: (await overview()).providers,
  config: await readFile(path.join(codex, "config.toml"), "utf8"),
  catalog: await readFile(
    path.join(codex, "uni-switch-models.json"),
    "utf8",
  ).catch((error) => {
    if (error.code === "ENOENT") return null;
    throw error;
  }),
});
const dialog = () =>
  page.getByRole("dialog", { name: "示例供应商 · 模型配置", exact: true });
const trigger = () =>
  page.getByRole("button", { name: "配置 示例供应商 的模型", exact: true });
async function openModels(selected = upstreamIds?.length ?? (many ? 85 : 5)) {
  await trigger().click();
  await dialog().waitFor();
  await dialog()
    .getByText(
      `已选择 ${selected} / ${upstreamIds?.length ?? (many ? 85 : 5)}`,
      { exact: true },
    )
    .waitFor();
}
async function audit(label) {
  const result = await new AxeBuilder({ page }).analyze();
  audits.push({
    label,
    violations: result.violations.map((v) => ({
      id: v.id,
      targets: v.nodes.map((n) => n.target),
    })),
  });
  assert.deepEqual(audits.at(-1).violations, []);
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
    env: (appEnv = {
      ...process.env,
      UNI_SWITCH_DATA_DIR: path.join(root, "data"),
      UNI_SWITCH_QA_DISCOVERY_CONTEXT: contextFile,
      WEBVIEW2_USER_DATA_FOLDER: path.join(root, "webview"),
      CODEX_HOME: codex,
      CLAUDE_CONFIG_DIR: cli,
      LOCALAPPDATA: local,
      UNI_SWITCH_QA_UPDATE_REPOSITORY: "example/uni-switch",
      UNI_SWITCH_QA_UPDATE_ENDPOINT: `http://127.0.0.1:${server.address().port}/github/releases/latest`,
      UNI_SWITCH_QA_UPDATE_OPEN_MARKER: path.join(root, "opened-release.txt"),
      UNI_SWITCH_QA_SERVICE_OPEN_MARKER: path.join(root, "opened-service.txt"),
      UNI_SWITCH_QA_PROJECT_OPEN_MARKER: path.join(root, "opened-project.txt"),
    }),
  });
  await until(portOpen);
  browser = await chromium.connectOverCDP("http://127.0.0.1:9223");
  page = browser.contexts()[0].pages()[0];
  page.setDefaultTimeout(15000);
  page.on("pageerror", (error) => errors.push(error.message));
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.setViewportSize({ width: 1120, height: 780 });
  await page.getByRole("button", { name: "设置", exact: true }).waitFor();
  const provider = await invoke("save_provider", {
    input: {
      id: null,
      family: "codex",
      name: "示例供应商",
      baseUrl,
      apiKey: key,
      authMode: "bearer",
      model: modelIds[0],
      reasoningEffort: null,
      codexOptions: {
        upstreamProtocol: "openai",
        models: [
          {
            id: modelIds[0],
            enabled: true,
            contextWindow: 256000,
            reasoningEfforts: [],
          },
        ],
        modelsSyncedAt: null,
        fastMode: false,
        contextWindow: null,
        autoCompactTokenLimit: null,
        balanceQuery: null,
      },
    },
  });
  await page.getByRole("button", { name: "刷新配置状态", exact: true }).click();
  await trigger()
    .getByText("已启用 1 个 · 管理模型", { exact: true })
    .waitFor();
  await until(
    async () =>
      !!(await overview()).providers.find((p) => p.id === provider.id)
        ?.codexOptions.protocolDetectedAt,
  );
  const headerBefore = await snapshot();
  const dismissNotice = page.getByRole("button", {
    name: "关闭提示",
    exact: true,
  });
  if (await dismissNotice.count()) await dismissNotice.click();
  await verifyWorkspaceHeader({ page, checks, audit, version });
  assert.deepEqual(await snapshot(), headerBefore);
  const before = await snapshot();
  await openModels();
  assert.equal(
    await page.locator(".provider-card .model-selection").count(),
    0,
  );
  assert.equal(
    await dialog()
      .getByText("当前已保存 1 个，保存后启用 5 个", { exact: true })
      .count(),
    1,
  );
  assert.equal(
    await dialog()
      .getByRole("checkbox", { name: /^启用 /, checked: true })
      .count(),
    5,
  );
  assert.deepEqual(await snapshot(), before);
  checks.push(
    "已保存一个模型、同步五个模型显示本次选择及保存前后数量，打开弹窗不写入",
  );
  await audit("Five-model dialog");
  await page.screenshot({
    path: `docs/screenshots/model-dialog-${version}.png`,
  });
  await page
    .locator(".modal-overlay")
    .click({ position: { x: 3, y: 3 }, force: true });
  assert.equal(await dialog().isVisible(), true);
  await dialog()
    .getByRole("button", { name: "保存模型配置", exact: true })
    .focus();
  await page.keyboard.press("Tab");
  assert.equal(
    await dialog()
      .getByRole("button", { name: "关闭弹窗" })
      .evaluate((el) => el === document.activeElement),
    true,
  );
  await page.keyboard.press("Escape");
  await dialog().waitFor({ state: "hidden" });
  assert.equal(
    await trigger().evaluate((el) => el === document.activeElement),
    true,
  );
  assert.deepEqual(await snapshot(), before);
  checks.push(
    "弹窗焦点循环、点击遮罩保留草稿、Escape取消并返回入口，数据库及文件不变",
  );
  await openModels();
  await dialog()
    .getByRole("button", { name: "保存模型配置", exact: true })
    .click();
  await dialog().waitFor({ state: "hidden" });
  await trigger()
    .getByText("已启用 5 个 · 管理模型", { exact: true })
    .waitFor();
  assert.equal(
    (await overview()).providers
      .find((p) => p.id === provider.id)
      .codexOptions.models.filter((m) => m.enabled).length,
    5,
  );
  checks.push("确认保存后数据库与列表均启用五个模型，下次打开显示一致");
  await page.getByRole("button", { name: "使用", exact: true }).click();
  await page
    .getByRole("dialog", { name: "重启 Codex 使配置生效", exact: true })
    .waitFor();
  await page.getByRole("button", { name: "稍后重启", exact: true }).click();
  let catalog = JSON.parse(
    await readFile(path.join(codex, "uni-switch-models.json"), "utf8"),
  );
  assert.deepEqual(
    catalog.models.map((m) => m.slug).sort(),
    [...modelIds].sort(),
  );
  for (const model of catalog.models)
    assert.deepEqual(model.input_modalities, ["text", "image"]);
  await verifyCodexModels({
    codexHome: codex,
    expected: Object.fromEntries(
      catalog.models.map((m) => [m.slug, m.context_window]),
    ),
    imageModels: modelIds,
    checkDesktopReasoning: false,
    output: path.join(root, "codex-image-input.json"),
  });
  for (const id of modelIds)
    assert.ok(
      inferenceRequests.some(
        (r) =>
          r.model === id && JSON.stringify(r.input).includes("input_image"),
      ),
      `${id} sends images to the mock upstream`,
    );
  checks.push(
    "真实 Codex app-server 对五个官方 GPT/Codex 模型逐个发送图片且完成响应，上游收到 input_image；不使用真实 API Key 或付费请求",
  );
  const appliedBefore = await snapshot();
  await openModels();
  await dialog().getByLabel(`启用 ${modelIds[1]}`, { exact: true }).uncheck();
  await dialog().getByRole("button", { name: "取消", exact: true }).click();
  assert.deepEqual(await snapshot(), appliedBefore);
  checks.push(
    "应用五个模型后Codex目录实际包含五个，取消当前供应商的模型草稿不修改文件",
  );
  await openModels();
  await dialog().getByLabel(`启用 ${modelIds[1]}`, { exact: true }).uncheck();
  await dialog()
    .getByLabel(`上下文长度 ${modelIds[0]}`, { exact: true })
    .fill("512");
  await dialog()
    .getByRole("button", { name: "保存并应用", exact: true })
    .click();
  await page
    .getByRole("dialog", { name: "重启 Codex 使配置生效", exact: true })
    .waitFor();
  await page.getByRole("button", { name: "稍后重启", exact: true }).click();
  await trigger()
    .getByText("已启用 4 个 · 管理模型", { exact: true })
    .waitFor();
  catalog = JSON.parse(
    await readFile(path.join(codex, "uni-switch-models.json"), "utf8"),
  );
  assert.equal(catalog.models.length, 4);
  assert.equal(
    catalog.models.find((m) => m.slug === modelIds[0]).context_window,
    512000,
  );
  checks.push(
    "弹窗确认修改后列表与Codex均为四个模型，上下文512k写入且提示重启",
  );
  await openModels(4);
  await dialog().getByLabel(`启用 ${modelIds[1]}`, { exact: true }).check();
  await dialog()
    .getByRole("button", { name: "保存并应用", exact: true })
    .click();
  await page
    .getByRole("dialog", { name: "重启 Codex 使配置生效", exact: true })
    .waitFor();
  await page.getByRole("button", { name: "稍后重启", exact: true }).click();
  many = true;
  await openModels();
  const largeBefore = await snapshot();
  for (const size of [
    { width: 1120, height: 780 },
    { width: 560, height: 520 },
    { width: 390, height: 620 },
  ]) {
    await page.setViewportSize(size);
    assert.equal(
      await dialog().evaluate((el) => el.scrollWidth > el.clientWidth),
      false,
    );
    const rect = await dialog()
      .getByRole("button", { name: "保存并应用", exact: true })
      .boundingBox();
    assert.ok(rect.y >= 0 && rect.y + rect.height <= size.height);
    const body = dialog().locator(".model-config-body");
    assert.equal(
      await body.evaluate((el) => el.scrollHeight > el.clientHeight),
      true,
    );
    await body.evaluate((el) => {
      el.scrollTop = el.scrollHeight;
    });
    await audit(`Large model dialog ${size.width}x${size.height}`);
  }
  checks.push(
    "85个模型分页计数正确，桌面及390px窄窗无横向溢出，模型单独滚动、保存按钮可见",
  );
  await page.screenshot({
    path: `docs/screenshots/model-dialog-narrow-${version}.png`,
  });
  await page.keyboard.press("Escape");
  assert.deepEqual(await snapshot(), largeBefore);
  await page.setViewportSize({ width: 1120, height: 780 });
  await verifyAppUpdates({
    page,
    invoke,
    snapshot,
    root,
    version,
    checks,
    audit,
    mock: updateMock,
  });
  assert.deepEqual(errors, []);
  await verifyBrandRefresh({ page, snapshot, root, checks, audit });
  const sourceSnapshot = await snapshot();
  await page.getByRole("button", { name: "设置", exact: true }).click();
  const settings = page.getByRole("dialog", { name: / 设置$/, exact: true });
  await settings
    .getByText("uni-switch · AGPL-3.0-only", { exact: true })
    .waitFor();
  for (const [label, url] of [
    ["查看源码 ↗", "https://github.com/dieqiyun/uni-switch"],
    ["完整许可 ↗", "https://github.com/dieqiyun/uni-switch/blob/main/LICENSE"],
  ]) {
    const link = settings.getByRole("link", { name: label, exact: true });
    assert.equal(await link.getAttribute("href"), url);
    await link.click();
    await until(
      async () =>
        (await readFile(path.join(root, "opened-project.txt"), "utf8").catch(
          () => "",
        )) === url,
    );
  }
  await assert.rejects(
    invoke("open_project_page", { page: "https://example.com" }),
  );
  await settings
    .getByText("Copyright © 2026 dieqiyun and uni-switch contributors.", {
      exact: true,
    })
    .waitFor();
  await audit("AGPL notices and source links");
  await page.screenshot({ path: "docs/screenshots/open-source-settings.png" });
  await settings.getByRole("button", { name: "关闭弹窗", exact: true }).click();
  assert.deepEqual(await snapshot(), sourceSnapshot);
  checks.push(
    "设置展示AGPL版权与无保证声明，源码及许可使用固定原生跳转；拒绝任意URL且不改变客户端配置",
  );

  await verifyTutorial({ page, snapshot, root, checks, audit });

  const catalogExpected = (saved) =>
    Object.fromEntries(saved.models.map((m) => [m.slug, m.context_window]));
  catalog = JSON.parse(
    await readFile(path.join(codex, "uni-switch-models.json"), "utf8"),
  );
  await verifyCodexModels({
    codexHome: codex,
    expected: catalogExpected(catalog),
    output: path.join(root, "codex-model-list-before.json"),
    listOnly: true,
  });
  many = false;
  upstreamIds = ["DLM-v1", "deepseek-v4-flash-0731", "qwen-coder", "glm-new"];
  const oldSnapshot = await snapshot();
  await openModels();
  for (const id of modelIds) {
    assert.equal(
      await dialog().getByLabel(`启用 ${id}`, { exact: true }).count(),
      0,
    );
    assert.equal(
      await dialog().getByLabel(`上下文长度 ${id}`, { exact: true }).count(),
      0,
    );
  }
  assert.equal(
    await dialog().getByText("最新列表未包含", { exact: true }).count(),
    0,
  );
  await dialog()
    .getByText("当前已保存 5 个，保存后启用 4 个", { exact: true })
    .waitFor();
  await audit("Retired models removed from draft");
  await page.screenshot({
    path: "docs/screenshots/model-catalog-pruned-local.png",
  });
  await dialog().getByRole("button", { name: "取消", exact: true }).click();
  assert.deepEqual(await snapshot(), oldSnapshot);
  await openModels();
  await dialog()
    .getByRole("button", { name: "保存并应用", exact: true })
    .click();
  await page
    .getByRole("dialog", { name: "重启 Codex 使配置生效", exact: true })
    .waitFor();
  await page.getByRole("button", { name: "稍后重启", exact: true }).click();
  await trigger()
    .getByText("已启用 4 个 · 管理模型", { exact: true })
    .waitFor();
  catalog = JSON.parse(
    await readFile(path.join(codex, "uni-switch-models.json"), "utf8"),
  );
  assert.deepEqual(
    catalog.models.map((m) => m.slug).sort(),
    [...upstreamIds].sort(),
  );
  const newest = (await overview()).providers.find((p) => p.id === provider.id);
  assert.deepEqual(
    newest.codexOptions.models.map((m) => m.id).sort(),
    [...upstreamIds].sort(),
  );
  assert.ok(upstreamIds.includes(newest.model));
  const codexList = await verifyCodexModels({
    codexHome: codex,
    expected: catalogExpected(catalog),
    output: path.join(root, "codex-model-list-after.json"),
    listOnly: true,
  });
  assert.ok(upstreamIds.includes(codexList.defaultModel));
  checks.push(
    "上游五个旧模型全部下架后只显示最新四个；取消不写入，保存并应用替换数据库/目录/默认模型并提示重启，真实Codex model/list仅返回最新四个",
  );
  const beforeCapabilities = await snapshot();
  await openModels();
  const imageField = dialog().getByLabel(`图片输入 ${upstreamIds[0]}`, {
    exact: true,
  });
  assert.equal(await imageField.isChecked(), false);
  await imageField.check();
  await dialog().getByRole("button", { name: "取消", exact: true }).click();
  assert.deepEqual(await snapshot(), beforeCapabilities);
  await openModels();
  await dialog()
    .getByLabel(`图片输入 ${upstreamIds[0]}`, { exact: true })
    .check();
  await dialog()
    .getByRole("button", { name: "刷新模型列表", exact: true })
    .click();
  await dialog().getByText("已选择 4 / 4", { exact: true }).waitFor();
  assert.equal(
    await dialog()
      .getByLabel(`图片输入 ${upstreamIds[0]}`, { exact: true })
      .isChecked(),
    true,
  );
  await audit("Manual unknown model capability");
  await page.screenshot({
    path: `docs/screenshots/model-capabilities-${version}.png`,
  });
  await page.setViewportSize({ width: 390, height: 620 });
  assert.equal(
    await dialog().evaluate((el) => el.scrollWidth > el.clientWidth),
    false,
  );
  await audit("Manual capability narrow layout");
  await page.screenshot({
    path: `docs/screenshots/model-capabilities-narrow-${version}.png`,
  });
  await page.setViewportSize({ width: 1120, height: 780 });
  await dialog()
    .getByRole("button", { name: "保存并应用", exact: true })
    .click();
  await page
    .getByRole("dialog", { name: "重启 Codex 使配置生效", exact: true })
    .waitFor();
  await page.getByRole("button", { name: "稍后重启", exact: true }).click();
  catalog = JSON.parse(
    await readFile(path.join(codex, "uni-switch-models.json"), "utf8"),
  );
  assert.deepEqual(
    catalog.models.find((m) => m.slug === upstreamIds[0]).input_modalities,
    ["text", "image"],
  );
  await verifyCodexModels({
    codexHome: codex,
    expected: catalogExpected(catalog),
    imageModels: [upstreamIds[0]],
    checkDesktopReasoning: false,
    output: path.join(root, "codex-manual-image-input.json"),
  });
  assert.ok(
    inferenceRequests.some(
      (r) =>
        r.model === upstreamIds[0] &&
        JSON.stringify(r.input).includes("input_image"),
    ),
  );
  checks.push(
    "未知模型默认提示待确认；手动图片能力取消不写入，刷新保留草稿，保存并提示重启；真实Codex成功发送此手动启用模型的图片",
  );
  await openModels();
  const retainedSnapshot = await snapshot();
  modelFailure = true;
  await dialog()
    .getByRole("button", { name: "刷新模型列表", exact: true })
    .click();
  await dialog().locator(".balance-failure").waitFor();
  await dialog().getByText("已选择 4 / 4", { exact: true }).waitFor();
  for (const id of modelIds)
    assert.equal(
      await dialog().getByLabel(`启用 ${id}`, { exact: true }).count(),
      0,
    );
  await audit("Failed model refresh retains latest catalog");
  await dialog().getByRole("button", { name: "取消", exact: true }).click();
  assert.deepEqual(await snapshot(), retainedSnapshot);
  checks.push(
    "模型请求失败保留已确认的最新列表，不复活下架模型，也不修改客户端文件",
  );
  const configBeforeUpgrade = await readFile(
    path.join(codex, "config.toml"),
    "utf8",
  );
  await browser.close();
  browser = null;
  await new Promise((resolve) => {
    app.once("exit", resolve);
    app.kill();
  });
  await until(async () => !(await portOpen()));
  execFileSync(
    "python",
    [
      "-X",
      "utf8",
      "-c",
      String.raw`import sys,json,sqlite3,pathlib
root=pathlib.Path(sys.argv[1]); db=sqlite3.connect(root/'data'/'uni-switch.db')
baseline=json.loads(db.execute("SELECT baseline FROM targets WHERE id='codex'").fetchone()[0])
file=next(f for f in baseline if f['format']=='catalog'); p=pathlib.Path(file['path'])
cat=json.loads(p.read_text(encoding='utf-8'))
for m in cat['models']: m['input_modalities']=['text']
text=json.dumps(cat,ensure_ascii=False,indent=2)+'\n'; p.write_text(text,encoding='utf-8',newline=''); assert p.read_bytes().decode('utf-8')==text; file['expected']=text
db.execute("UPDATE targets SET baseline=? WHERE id='codex'",[json.dumps(baseline,ensure_ascii=False)]); db.commit(); db.close()`,
      root,
    ],
    { windowsHide: true, encoding: "utf8" },
  );
  app = spawn(executable, [], {
    windowsHide: true,
    stdio: "ignore",
    env: appEnv,
  });
  await until(portOpen);
  browser = await chromium.connectOverCDP("http://127.0.0.1:9223");
  page = browser.contexts()[0].pages()[0];
  page.setDefaultTimeout(15000);
  page.on("pageerror", (error) => errors.push(error.message));
  await page.setViewportSize({ width: 1120, height: 780 });
  await page
    .getByRole("dialog", { name: "重启 Codex 使配置生效", exact: true })
    .waitFor();
  assert.equal((await overview()).repairedModelCapabilities, true);
  assert.equal(
    await readFile(path.join(codex, "config.toml"), "utf8"),
    configBeforeUpgrade,
  );
  catalog = JSON.parse(
    await readFile(path.join(codex, "uni-switch-models.json"), "utf8"),
  );
  assert.deepEqual(
    catalog.models.find((m) => m.slug === upstreamIds[0]).input_modalities,
    ["text", "image"],
  );
  await audit("Automatic old catalog repair restart dialog");
  await page.screenshot({
    path: `docs/screenshots/model-capability-upgrade-${version}.png`,
  });
  await page.getByRole("button", { name: "稍后重启", exact: true }).click();
  checks.push(
    "原生客户端重新启动时自动修复旧版受管的text-only目录；配置与密钥不变且自动弹出Codex重启提醒",
  );
  assert.deepEqual(errors, []);
  const result = { version, root, checks, audits, pageErrors: errors };
  await writeFile(
    path.join(root, "results.json"),
    JSON.stringify(result, null, 2),
  );
  await writeFile(
    path.resolve(".qa/model-dialog/results.json"),
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
  if (app && app.exitCode === null) {
    await new Promise((resolve) => {
      app.once("exit", resolve);
      app.kill();
    });
  }
  server.closeAllConnections();
  await new Promise((resolve) => server.close(resolve));
}
