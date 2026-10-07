import { chromium } from "playwright";
import AxeBuilder from "@axe-core/playwright";
import http from "node:http";
import { spawn } from "node:child_process";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import assert from "node:assert/strict";
import {
  mockModelResponse,
  verifyCodexModels,
} from "./qa-codex-model-runtime.mjs";

const root = path.resolve(".qa/auto-models/native");
const runDirectory = path.join(root, Date.now().toString());
const codexHome = path.join(runDirectory, "Codex 中文");
const claudeHome = path.join(runDirectory, "claude-desktop");
const cliHome = path.join(runDirectory, "claude-cli");
await mkdir(codexHome, { recursive: true });
await mkdir(claudeHome, { recursive: true });
await mkdir("docs/screenshots", { recursive: true });
const key = "isolated-models-key";
const requests = [],
  checks = [],
  accessibility = [],
  errors = [],
  inferenceRequests = [];
let models = ["gpt-6.1-sol", "gpt-5.5", "text-embedding-3-large"],
  failure = false;
const server = http.createServer((req, res) => {
  const url = new URL(req.url, "http://local.test");
  const [, site, ...parts] = url.pathname.split("/");
  const route = parts.join("/");
  if (route === "v1/responses") {
    if (req.headers.authorization !== `Bearer ${key}`) {
      res.writeHead(401);
      res.end("{}");
      return;
    }
    void mockModelResponse(req, res, inferenceRequests);
    return;
  }
  requests.push({
    site,
    route,
    after: url.searchParams.get("after_id"),
    bearer: req.headers.authorization === `Bearer ${key}`,
    nativeKey: req.headers["x-api-key"] === key,
    anthropicVersion: req.headers["anthropic-version"] || null,
  });
  const send = (body, code = 200) => {
    res.writeHead(code, { "Content-Type": "application/json" });
    res.end(JSON.stringify(body));
  };
  if (route === "v1/usage")
    return send({ mode: "unrestricted", balance: 12.5, unit: "USD" });
  if (route.endsWith("models")) {
    if (
      site === "denied" ||
      failure ||
      req.headers.authorization === "Bearer wrong-key"
    )
      return send({ error: "do-not-echo-private-secret" }, 403);
    if (site === "empty") return send({ data: [] });
    if (["claude", "fallback"].includes(site) && route === "models")
      return send({}, 404);
    if (site === "claude") {
      if (
        req.headers["x-api-key"] !== key ||
        req.headers["anthropic-version"] !== "2023-06-01"
      )
        return send({}, 401);
      const after = url.searchParams.get("after_id");
      return send(
        after
          ? { data: [{ id: "claude-sonnet-4-6" }], has_more: false }
          : {
              data: [{ id: "claude-opus-4-6" }, { id: "gpt-unsupported" }],
              has_more: true,
              last_id: "gpt-unsupported",
            },
      );
    }
    const ids =
      site === "changed"
        ? ["new-model"]
        : site === "many"
          ? Array.from(
              { length: 20 },
              (_, i) => `model-${String(i).padStart(2, "0")}`,
            )
          : models;
    if (site === "slow") {
      setTimeout(() => send({ data: [{ id: "stale-model" }] }), 1600);
      return;
    }
    return send({ data: ids.map((id) => ({ id })) });
  }
  send({}, 404);
});
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const origin = `http://127.0.0.1:${server.address().port}`;
const base = (site) =>
  `${origin}/${site}${site === "claude" || site === "fallback" ? "" : "/v1"}`;
const waitUntil = async (fn, timeout = 20000) => {
  const end = Date.now() + timeout;
  while (Date.now() < end) {
    if (await fn()) return;
    await new Promise((r) => setTimeout(r, 150));
  }
  throw Error("Timed out waiting for native QA");
};
let child, browser, page;
try {
  let occupied = false;
  try {
    occupied = (await fetch("http://127.0.0.1:9223/json/version")).ok;
  } catch {}
  assert.equal(occupied, false, "QA port must not belong to another process");
  child = spawn(
    path.resolve(
      process.env.UNI_SWITCH_QA_EXE ||
        ".qa/auto-models/test-app/uni-switch.exe",
    ),
    [],
    {
      windowsHide: true,
      stdio: "ignore",
      env: {
        ...process.env,
        UNI_SWITCH_DATA_DIR: path.join(runDirectory, "data"),
        WEBVIEW2_USER_DATA_FOLDER: path.join(runDirectory, "webview"),
        CODEX_HOME: codexHome,
        CLAUDE_CONFIG_DIR: cliHome,
        LOCALAPPDATA: claudeHome,
      },
    },
  );
  await waitUntil(async () => {
    try {
      return (await fetch("http://127.0.0.1:9223/json/version")).ok;
    } catch {
      return false;
    }
  });
  browser = await chromium.connectOverCDP("http://127.0.0.1:9223");
  page = browser.contexts()[0].pages()[0];
  page.setDefaultTimeout(12000);
  page.on("pageerror", (e) => errors.push(e.message));
  await page.getByRole("button", { name: /^添加(第一组)?配置$/ }).waitFor();
  const invoke = (name, args = {}) =>
    page.evaluate(
      ({ name, args }) => window.__TAURI_INTERNALS__.invoke(name, args),
      { name, args },
    );
  const checkbox = (id) =>
    page.getByRole("checkbox", { name: `启用 ${id}`, exact: true });
  const add = async (site) => {
    console.log(`QA: ${site}`);
    await page.getByRole("button", { name: /^添加(第一组)?配置$/ }).click();
    await page.locator("#baseUrl").fill(base(site));
    await page.locator("#apiKey").fill(key);
  };
  const name = async (text) => {
    await page.locator(".form-advanced > summary").click();
    await page.locator("#name").fill(text);
    await page.locator(".form-advanced > summary").click();
  };
  const save = async () => {
    await page.getByRole("button", { name: "保存配置", exact: true }).click();
    await page.getByRole("dialog").waitFor({ state: "hidden" });
  };
  const close = () =>
    page.getByRole("button", { name: "取消", exact: true }).click();
  const refresh = async () => {
    const button = page.getByRole("button", {
      name: "刷新模型列表",
      exact: true,
    });
    await waitUntil(() => button.isEnabled());
    await button.click();
    await waitUntil(() => button.isEnabled());
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
    const a = await new AxeBuilder({ page }).analyze();
    accessibility.push({
      label,
      violations: a.violations.map((v) => ({
        id: v.id,
        nodes: v.nodes.map((n) => n.target),
      })),
    });
    await writeFile(
      path.join(root, "accessibility.json"),
      JSON.stringify(accessibility, null, 2),
    );
    assert.deepEqual(
      a.violations.map((v) => v.id),
      [],
      label,
    );
  };

  await add("codex");
  assert.equal(await page.locator("input#model, select#model").count(), 0);
  await checkbox("gpt-6.1-sol").waitFor();
  assert.equal(await checkbox("gpt-5.5").isChecked(), true);
  assert.equal(
    await page.getByLabel("上下文长度 gpt-5.5", { exact: true }).inputValue(),
    "256",
  );
  assert.equal(
    await page
      .getByLabel("上下文长度 gpt-6.1-sol", { exact: true })
      .inputValue(),
    "256",
  );
  await page.getByLabel("上下文长度 gpt-6.1-sol", { exact: true }).fill("512");
  assert.equal(
    await page.locator("#contextWindow, #autoCompactTokenLimit").count(),
    0,
  );
  assert.equal(await checkbox("gpt-6.1-sol").isChecked(), false);
  await checkbox("gpt-6.1-sol").click();
  await page
    .getByRole("button", { name: "将 gpt-6.1-sol 设为默认模型", exact: true })
    .click();
  await checkbox("gpt-6.1-sol").click();
  await checkbox("gpt-5.5").click();
  await page.getByRole("button", { name: "保存配置", exact: true }).click();
  await page.getByText("请至少勾选一个要启用的模型", { exact: true }).waitFor();
  await checkbox("gpt-6.1-sol").click();
  await checkbox("gpt-5.5").click();
  await page
    .getByRole("button", { name: "将 gpt-5.5 设为默认模型", exact: true })
    .click();
  await audit("Codex model selection");
  await page.locator(".model-selection").scrollIntoViewIfNeeded();
  await page.getByRole("dialog").evaluate((el) => {
    el.scrollTop = 210;
  });
  await page.screenshot({ path: "docs/screenshots/auto-models-form.png" });
  await page
    .locator(".model-selection")
    .screenshot({ path: "docs/screenshots/auto-models-section.png" });
  await name("自动模型供应商");
  await save();
  let overview = await invoke("get_overview");
  const provider = overview.providers.find((p) => p.name === "自动模型供应商");
  assert.equal(provider.model, "gpt-5.5");
  assert.deepEqual(
    provider.codexOptions.models.filter((m) => m.enabled).map((m) => m.id),
    ["gpt-5.5", "gpt-6.1-sol"],
  );
  await invoke("apply_provider", { target: "codex", providerId: provider.id });
  // Direct backend invocation bypasses React Query invalidation; reload the
  // isolated UI so editing receives the current applied-provider state.
  await page.reload();
  const config = await readFile(path.join(codexHome, "config.toml"), "utf8");
  assert.ok(config.includes('model = "gpt-5.5"'));
  const catalog = JSON.parse(
    await readFile(path.join(codexHome, "uni-switch-models.json"), "utf8"),
  );
  assert.deepEqual(catalog.models.map((m) => m.slug).sort(), [
    "gpt-5.5",
    "gpt-6.1-sol",
  ]);
  for (const model of catalog.models) {
    const context = model.slug === "gpt-6.1-sol" ? 512000 : 256000;
    assert.equal(model.context_window, context);
    assert.equal(model.auto_compact_token_limit, context);
  }
  assert.equal(config.includes("model_context_window"), false);
  checks.push(
    "只填地址与 Key 自动同步；勾选、切换默认、全部取消校验；只将启用模型写入 Codex",
  );

  await page
    .getByRole("button", { name: "编辑 自动模型供应商", exact: true })
    .click();
  await checkbox("gpt-5.5").waitFor();
  models.push("gpt-new");
  await refresh();
  assert.equal(await checkbox("gpt-new").isChecked(), false);
  assert.equal(await checkbox("gpt-5.5").isChecked(), true);
  assert.equal(
    await page
      .getByLabel("上下文长度 gpt-6.1-sol", { exact: true })
      .inputValue(),
    "512",
  );
  await checkbox("gpt-new").click();
  await page.getByLabel("上下文长度 gpt-5.5", { exact: true }).fill("128");
  await waitUntil(async () => {
    const current = JSON.parse(
      await readFile(path.join(codexHome, "uni-switch-models.json"), "utf8"),
    );
    return (
      current.models.length === 3 &&
      current.models.find((m) => m.slug === "gpt-5.5")?.context_window ===
        128000
    );
  });
  await page
    .getByText("模型列表已自动写入 Codex，重启 Codex 后加载。", { exact: true })
    .waitFor();
  await page.getByRole("dialog").evaluate((el) => {
    const section = el.querySelector(".model-selection");
    el.scrollTop +=
      section.getBoundingClientRect().top - el.getBoundingClientRect().top - 80;
  });
  await page
    .locator(".model-selection")
    .screenshot({ path: "docs/screenshots/model-context-section.png" });
  const runtime = await verifyCodexModels({
    codexHome,
    expected: { "gpt-5.5": 128000, "gpt-6.1-sol": 512000, "gpt-new": 256000 },
    output: path.join(root, "codex-runtime.json"),
  });
  assert.deepEqual(
    inferenceRequests.map((r) => r.model),
    runtime.models,
  );
  checks.push(
    "每模型默认256k，可单独编辑且刷新保留；当前供应商自动写入新增启用模型和长度；真实Codex识别三模型，同一会话切换模型分别使用128k/512k/256k上下文",
  );
  failure = true;
  await refresh();
  await page.getByText(/可继续使用已保存的模型/).waitFor();
  await save();
  failure = false;
  checks.push(
    "编辑复用已保存 Key，刷新保留勾选和默认，新模型不自动启用；同步失败仍可保存原配置",
  );

  await add("slow");
  await page
    .getByText("正在获取这把密钥可用的模型…", { exact: true })
    .waitFor();
  assert.equal(await page.locator("#baseUrl").isEnabled(), true);
  await page.locator("#baseUrl").fill(base("changed"));
  await checkbox("new-model").waitFor();
  await page.waitForTimeout(1700);
  assert.equal(await checkbox("stale-model").count(), 0);
  await page.locator("#apiKey").fill("wrong-key");
  await page
    .locator(".model-selection")
    .getByText(/HTTP 403/)
    .waitFor();
  assert.equal(await checkbox("new-model").count(), 0);
  await close();
  checks.push("同步不锁定表单；更换地址与 Key 丢弃旧模型和旧响应");

  for (const site of ["empty", "denied"]) {
    await add(site);
    await page
      .locator(".model-selection")
      .getByText(site === "empty" ? /没有返回可用模型/ : /HTTP 403/)
      .waitFor();
    await page.getByRole("button", { name: "保存配置", exact: true }).click();
    await page
      .getByText("请至少勾选一个要启用的模型", { exact: true })
      .waitFor();
    assert.equal(await page.getByText(/do-not-echo-private-secret/).count(), 0);
    await close();
  }
  await add("fallback");
  await checkbox("gpt-6.1-sol").waitFor();
  await close();
  checks.push("空模型和权限失败不会虚构模型或保存；根地址自动尝试 /v1/models");

  await add("many");
  await checkbox("model-00").waitFor();
  await page
    .getByRole("textbox", { name: "搜索模型", exact: true })
    .fill("model-18");
  await checkbox("model-18").focus();
  await page.keyboard.press("Space");
  assert.equal(await checkbox("model-18").isChecked(), true);
  await audit("Search and keyboard checkbox");
  await page.setViewportSize({ width: 760, height: 600 });
  await page.getByRole("dialog").evaluate((el) => {
    el.scrollTop = el.scrollHeight;
  });
  assert.equal(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    ),
    false,
  );
  await audit("Small model form");
  await page.setViewportSize({ width: 390, height: 844 });
  await page
    .getByLabel("上下文长度 model-18", { exact: true })
    .scrollIntoViewIfNeeded();
  await page.getByLabel("上下文长度 model-18", { exact: true }).fill("384");
  assert.equal(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    ),
    false,
  );
  await audit("Narrow per-model context form");
  await page
    .locator(".model-selection")
    .screenshot({ path: "docs/screenshots/model-context-narrow.png" });
  await close();
  await page.setViewportSize({ width: 1120, height: 780 });
  checks.push("大量模型可搜索；键盘勾选、小窗口和 Axe 验证通过");

  await page.getByRole("tab", { name: /Claude Code/ }).click();
  await add("claude");
  await page.locator(".form-advanced > summary").click();
  await page.locator("#authMode").selectOption("x-api-key");
  await page.locator("#name").fill("Claude 自动模型");
  await page.locator(".form-advanced > summary").click();
  await checkbox("claude-sonnet-4-6").waitFor();
  assert.equal(await checkbox("gpt-unsupported").isDisabled(), true);
  await checkbox("claude-sonnet-4-6").click();
  await page
    .getByRole("button", {
      name: "将 claude-sonnet-4-6 设为默认模型",
      exact: true,
    })
    .click();
  await audit("Claude multi-model form");
  await save();
  overview = await invoke("get_overview");
  const claude = overview.providers.find((p) => p.name === "Claude 自动模型");
  await invoke("apply_provider", {
    target: "claude_desktop",
    providerId: claude.id,
  });
  const profile = JSON.parse(
    await readFile(
      path.join(
        claudeHome,
        "Claude-3p/configLibrary/e82de475-47fa-4c54-9000-13571c000001.json",
      ),
      "utf8",
    ),
  );
  assert.deepEqual(profile.inferenceModels, [
    "claude-sonnet-4-6",
    "claude-opus-4-6",
  ]);
  await invoke("apply_provider", {
    target: "claude_cli",
    providerId: claude.id,
  });
  const cli = JSON.parse(
    await readFile(path.join(cliHome, "settings.json"), "utf8"),
  );
  assert.equal(cli.env.ANTHROPIC_MODEL, "claude-sonnet-4-6");
  assert.equal(cli.env.ANTHROPIC_DEFAULT_OPUS_MODEL, "claude-opus-4-6");
  assert.ok(
    requests.some(
      (r) =>
        r.site === "claude" &&
        r.after === "gpt-unsupported" &&
        r.nativeKey &&
        r.anthropicVersion === "2023-06-01",
    ),
  );
  checks.push(
    "Claude x-api-key 认证、自动分页合并、禁用不支持模型、桌面多模型写入与 CLI 默认/档位映射",
  );
  assert.deepEqual(errors, []);
  await writeFile(
    path.join(root, "results.json"),
    JSON.stringify(
      {
        version: "0.3.6",
        passed: checks.length,
        checks,
        requests,
        accessibility,
        pageErrors: errors,
        runDirectory,
      },
      null,
      2,
    ),
  );
  console.log(JSON.stringify({ passed: checks.length, checks }, null, 2));
} catch (e) {
  if (page)
    console.error(
      await page
        .locator("body")
        .innerText()
        .catch(() => "Page unavailable"),
    );
  if (page)
    await page
      .screenshot({ path: path.join(root, "failure.png") })
      .catch(() => {});
  throw e;
} finally {
  if (browser) await browser.close();
  if (child && child.exitCode === null)
    await new Promise((resolve) => {
      child.once("exit", resolve);
      child.kill();
    });
  await new Promise((resolve) => server.close(resolve));
}
