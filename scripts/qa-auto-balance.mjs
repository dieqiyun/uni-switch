import { chromium } from "playwright";
import AxeBuilder from "@axe-core/playwright";
import http from "node:http";
import { spawn } from "node:child_process";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import assert from "node:assert/strict";

// Self-contained Tauri build: --features qa-webview,tauri/custom-protocol.
// All credentials, supplier endpoints, databases and client files are isolated.
const root = path.resolve(".qa/auto-balance/native");
const runDirectory = path.join(root, Date.now().toString());
const codexHome = path.join(runDirectory, "Codex 中文");
await mkdir(codexHome, { recursive: true });
await mkdir("docs/screenshots", { recursive: true });
const inferenceKey = "sk-auto-balance-isolated-key";
const consoleToken = "isolated-legacy-console-token";
const requests = [], checks = [], errors = [], accessibility = [];
let subMode = "wallet", billingMode = "wallet";
const server = http.createServer((req, res) => {
  const [, site, ...parts] = req.url.split("/");
  const route = parts.join("/");
  requests.push({
    site, route,
    inferenceAuth: req.headers.authorization === `Bearer ${inferenceKey}`,
    consoleAuth: req.headers.authorization === `Bearer ${consoleToken}`,
    noAuth: !req.headers.authorization,
    userId: req.headers["new-api-user"] || null,
  });
  const send = (body, status = 200) => {
    res.writeHead(status, { "Content-Type": "application/json" });
    res.end(JSON.stringify(body));
  };
  if (route === "v1/models") return send({ data: [
    { id: "test-model" }, { id: "claude-sonnet-4-6" }, { id: "claude-opus-4-6" },
  ] });
  if (site === "auth") return send({ error: "do-not-display-test-secret" }, 401);
  if (site === "unsupported") return send({}, 404);
  if (site === "slow" && route === "v1/usage") {
    setTimeout(() => send({ mode: "unrestricted", balance: 99, unit: "USD" }), 1700);
    return;
  }
  if (["sub2", "changed", "claude"].includes(site) && route === "v1/usage") {
    if (req.headers.authorization !== `Bearer ${inferenceKey}`)
      return send({ error: "wrong key" }, 403);
    if (site === "changed" || site === "claude")
      return send({ mode: "unrestricted", balance: 7, unit: "USD" });
    if (subMode === "wallet" || subMode === "zero")
      return send({ mode: "unrestricted", isValid: true, planName: "钱包余额",
        balance: subMode === "zero" ? 0 : 23.45, unit: "USD" });
    if (subMode === "quota")
      return send({ mode: "quota_limited", isValid: true, unit: "USD",
        quota: { limit: 100, used: 10, remaining: 90, unit: "USD" },
        rate_limits: [{ window: "5h", limit: 5, used: 2, remaining: 3 }] });
    if (subMode === "unlimited")
      return send({ mode: "unrestricted", isValid: true, remaining: -1,
        planName: "无限订阅", unit: "USD", subscription: {
          daily_limit_usd: null, weekly_limit_usd: null, monthly_limit_usd: null,
        } });
    return send({ mode: "unrestricted", isValid: true, remaining: 5,
      planName: "月度套餐", unit: "USD", subscription: {
        daily_limit_usd: 10, daily_usage_usd: 2,
        weekly_limit_usd: 50, weekly_usage_usd: 45,
        monthly_limit_usd: 200, monthly_usage_usd: 120,
        expires_at: "2026-11-01T00:00:00Z",
      } });
  }
  if (["newapi", "token", "legacy"].includes(site)) {
    if (route === "api/status")
      return send({ success: true, data: { quota_per_unit: 500000,
        quota_display_type: billingMode === "cny" ? "CNY" : "USD",
        usd_exchange_rate: 7 } });
    if (route === "api/usage/token/")
      return send({ code: true, data: { object: "token_usage", name: "测试密钥",
        total_available: site === "token" ? 0 : 6250000, total_used: 1000000,
        total_granted: site === "token" ? 1000000 : 7250000,
        unlimited_quota: billingMode === "unlimited", expires_at: 0 } });
    if (site === "legacy" && route === "api/user/self") {
      if (req.headers.authorization !== `Bearer ${consoleToken}` || req.headers["new-api-user"] !== "42")
        return send({ success: false }, 401);
      return send({ success: true, data: { quota: 4500000, used_quota: 1500000 } });
    }
    if (route === "v1/dashboard/billing/subscription") {
      if (site === "token") return send({ error: "exhausted key" }, 403);
      return send({ object: "billing_subscription", hard_limit_usd:
        billingMode === "unlimited" ? 100000000 : billingMode === "cny" ? 840 : 120 });
    }
    if (route === "v1/dashboard/billing/usage")
      return send({ object: "list", total_usage: billingMode === "cny" ? 14000 : 2000 });
  }
  if (site === "credit" && route === "v1/dashboard/billing/credit_grants")
    return send({ total_available: 4.5, total_used: 1, total_granted: 5.5 });
  if (site === "custom" && route === "custom-balance")
    return send({ data: { balance: "0" } });
  send({}, 404);
});
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const origin = `http://127.0.0.1:${server.address().port}`;
const base = (site) => `${origin}/${site}/v1`;
const executable = path.resolve(process.env.UNI_SWITCH_QA_EXE || ".qa/auto-balance/test-app/uni-switch.exe");
const waitUntil = async (fn, timeout = 20000) => {
  const end = Date.now() + timeout;
  while (Date.now() < end) {
    if (await fn()) return;
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error("Timed out waiting for QA desktop");
};
let child, browser, page;
try {
  let portOccupied = false;
  try { portOccupied = (await fetch("http://127.0.0.1:9223/json/version")).ok; } catch {}
  assert.equal(portOccupied, false, "QA port must be unoccupied before starting an owned app");
  child = spawn(executable, [], { windowsHide: true, stdio: "ignore", env: {
    ...process.env, UNI_SWITCH_DATA_DIR: path.join(runDirectory, "data"),
    WEBVIEW2_USER_DATA_FOLDER: path.join(runDirectory, "webview"),
    CODEX_HOME: codexHome, CLAUDE_CONFIG_DIR: path.join(runDirectory, "claude"),
  } });
  await waitUntil(async () => {
    try { return (await fetch("http://127.0.0.1:9223/json/version")).ok; } catch { return false; }
  });
  browser = await chromium.connectOverCDP("http://127.0.0.1:9223");
  page = browser.contexts()[0].pages()[0];
  page.setDefaultTimeout(12000);
  page.on("pageerror", (e) => errors.push(e.message));
  await page.getByRole("button", { name: /^添加(第一组)?配置$/ }).waitFor();
  const invoke = (name, args = {}) => page.evaluate(({ name, args }) =>
    window.__TAURI_INTERNALS__.invoke(name, args), { name, args });
  const card = (name) => page.getByRole("listitem").filter({ has: page.getByRole("heading", { name, exact: true }) });
  const add = async (name, site, model = "test-model") => {
    console.log(`QA: ${name}`);
    await page.getByRole("button", { name: /^添加(第一组)?配置$/ }).click();
    await page.locator("#baseUrl").fill(base(site));
    await page.locator("#apiKey").fill(inferenceKey);
    await page.getByRole("checkbox", { name: `启用 ${model}`, exact: true }).waitFor();
    await page.locator(".form-advanced > summary").click();
    await page.locator("#name").fill(name);
  };
  const save = async () => {
    await page.getByRole("button", { name: "保存配置", exact: true }).click();
    await page.getByRole("dialog").waitFor({ state: "hidden" });
  };
  const balanceRegion = () => page.locator(".auto-balance");
  const formResult = (text) => balanceRegion().getByText(text, { exact: true }).waitFor();
  const refresh = async (name) => {
    const button = card(name).getByRole("button", { name: `刷新 ${name} 余额`, exact: true });
    await waitUntil(() => button.isEnabled());
    await button.click();
    await waitUntil(() => button.isEnabled());
  };
  const audit = async (name) => {
    console.log(`Axe: ${name}`);
    await page.evaluate(async () => {
      await Promise.race([
        Promise.all(document.getAnimations().filter((a) => a.effect?.getComputedTiming().iterations !== Infinity)
          .map((a) => a.finished.catch(() => {}))),
        new Promise((resolve) => setTimeout(resolve, 300)),
      ]);
    });
    const result = await new AxeBuilder({ page }).analyze();
    accessibility.push({ name, violations: result.violations.map((v) => ({ id: v.id, impact: v.impact,
      nodes: v.nodes.map((n) => ({ target: n.target, summary: n.failureSummary })) })) });
    await writeFile(path.join(root, "accessibility.json"), JSON.stringify(accessibility, null, 2));
    assert.deepEqual(result.violations.map((v) => v.id), [], `${name}: accessibility`);
  };

  await add("Sub2API 自动识别", "sub2");
  assert.equal(await page.locator("#balancePreset, #balancePath, #balanceSiteUrl, #balanceUserId, #balanceAccessToken").count(), 0);
  await formResult("账户余额 · 23.45 USD");
  await balanceRegion().getByText("Sub2API", { exact: true }).waitFor();
  await page.locator(".form-advanced > summary").click();
  await balanceRegion().scrollIntoViewIfNeeded();
  // The footer is sticky; scroll to the end so it cannot cover the result.
  await page.getByRole("dialog").evaluate((el) => { el.scrollTop = el.scrollHeight; });
  await audit("自动查询表单");
  await page.screenshot({ path: "docs/screenshots/auto-balance-form.png" });
  await balanceRegion().screenshot({ path: "docs/screenshots/auto-balance-section.png" });
  await save();
  await card("Sub2API 自动识别").getByText("账户余额 · 23.45 USD", { exact: true }).waitFor();
  checks.push("只填地址与 Key 自动查询；移除开关、类型、站点和额外认证字段；保存后列表自动加载");

  for (const [mode, text] of [["zero", "账户余额 · 0 USD"], ["quota", "密钥额度 · 90 USD"],
    ["subscription", "订阅额度 · 5 USD"], ["unlimited", "订阅额度 · 无限额度"]]) {
    subMode = mode;
    await refresh("Sub2API 自动识别");
    await card("Sub2API 自动识别").getByText(text, { exact: true }).waitFor();
  }
  checks.push("Sub2API 自动解析真实零余额、密钥额度、订阅周期和无限额度");

  await add("New API 自动识别", "newapi");
  await formResult("可用额度 · 100 USD");
  await balanceRegion().getByText("由站点决定返回账户余额或密钥额度", { exact: true }).waitFor();
  await save();
  await card("New API 自动识别").getByText("可用额度 · 100 USD", { exact: true }).waitFor();
  billingMode = "cny";
  await refresh("New API 自动识别");
  await card("New API 自动识别").getByText("可用额度 · 700 CNY", { exact: true }).waitFor();
  billingMode = "unlimited";
  await refresh("New API 自动识别");
  await card("New API 自动识别").getByText("可用额度 · 无限额度", { exact: true }).waitFor();
  billingMode = "wallet";
  await refresh("New API 自动识别");
  checks.push("New API 只用 API Key 自动组合 billing；正确计算差值和单位，余额范围由服务端决定");

  await add("New API 密钥额度", "token");
  await formResult("密钥额度 · 0 USD");
  await balanceRegion().getByText("当前为密钥额度；账户余额接口未返回可用结果", { exact: true }).waitFor();
  await save();
  await card("New API 密钥额度").getByText("密钥额度 · 0 USD", { exact: true }).waitFor();
  await add("兼容余额接口", "credit");
  await formResult("余额 · 4.5 USD");
  await save();
  checks.push("New API billing 拒绝查询时准确标注密钥额度；兼容 credit_grants 自动回退");

  await add("自动查询失败", "unsupported");
  await balanceRegion().getByText(/未找到可用的余额接口/).waitFor();
  assert.equal(await balanceRegion().getByText(/ · 0 USD/).count(), 0);
  await save();
  await card("自动查询失败").getByText("余额暂不可用", { exact: true }).waitFor();
  await add("权限拒绝", "auth");
  await balanceRegion().getByText(/余额接口拒绝访问/).waitFor();
  assert.equal(await page.getByText(/do-not-display-test-secret/).count(), 0);
  await page.getByRole("button", { name: "取消", exact: true }).click();
  checks.push("不支持、权限错误显示原因，不编造零余额、不阻止保存、不展示接口错误中的秘密");

  await add("地址切换", "slow");
  await balanceRegion().getByText("正在识别供应商并查询余额…", { exact: true }).waitFor();
  await page.locator("#baseUrl").fill(base("changed"));
  await formResult("账户余额 · 7 USD");
  await page.waitForTimeout(1800);
  assert.equal(await balanceRegion().getByText("账户余额 · 99 USD", { exact: true }).count(), 0);
  await page.locator("#apiKey").fill("wrong-test-key");
  await balanceRegion().getByText(/余额接口拒绝访问/).waitFor();
  assert.equal(await balanceRegion().getByText("账户余额 · 7 USD", { exact: true }).count(), 0);
  await page.locator("#apiKey").fill(inferenceKey);
  await formResult("账户余额 · 7 USD");
  await page.getByRole("button", { name: "取消", exact: true }).click();
  checks.push("修改地址或 Key 清空旧余额，旧慢响应无法覆盖新供应商结果");

  const legacy = await invoke("save_provider", { input: { id: null, family: "codex", name: "旧账户配置",
    baseUrl: base("legacy"), apiKey: inferenceKey, model: "test-model", authMode: "bearer", reasoningEffort: null,
    balanceAccessToken: consoleToken, codexOptions: { balanceQuery: { adapter: "newapi_account",
      path: "", jsonPath: "", siteUrl: null, userId: "42", unit: "USD", divisor: 500000 } } } });
  await invoke("save_provider", { input: { id: null, family: "codex", name: "旧自定义配置",
    baseUrl: base("custom"), apiKey: inferenceKey, model: "test-model", authMode: "bearer", reasoningEffort: null,
    codexOptions: { balanceQuery: { adapter: "custom", path: "/custom/custom-balance",
      jsonPath: "data.balance", unit: "USD", divisor: 1 } } } });
  await page.reload();
  await card("旧账户配置").getByText("账户余额 · 9 USD", { exact: true }).waitFor();
  await card("旧自定义配置").getByText("余额 · 0 USD", { exact: true }).waitFor();
  await page.getByRole("button", { name: "编辑 旧账户配置", exact: true }).click();
  await formResult("账户余额 · 9 USD");
  assert.equal(await page.locator("#balanceAccessToken, #balanceUserId, #balancePreset").count(), 0);
  await save();
  const overview = await invoke("get_overview");
  assert.ok(!JSON.stringify(overview).includes(consoleToken));
  await invoke("apply_provider", { target: "codex", providerId: legacy.id });
  const applied = await readFile(path.join(codexHome, "config.toml"), "utf8");
  assert.ok(applied.includes(inferenceKey) && !applied.includes(consoleToken));
  assert.ok(requests.filter((r) => r.route === "api/status").every((r) => r.noAuth && !r.userId));
  assert.ok(requests.filter((r) => r.route === "api/user/self").every((r) => r.consoleAuth && r.userId === "42"));
  assert.ok(requests.filter((r) => r.route === "api/usage/token/")
    .every((r) => !r.noAuth && !r.consoleAuth && !r.userId));
  assert.ok(requests.filter((r) => r.route === "api/usage/token/" && ["newapi", "token", "legacy"].includes(r.site))
    .every((r) => r.inferenceAuth));
  assert.ok(!requests.some((r) => r.route === "api/usage/token"));
  checks.push("旧账户与自定义配置自动兼容；推理 Key、控制台 Token 和公共查询严格分开，客户端只写入推理 Key");

  await page.getByRole("tab", { name: /Claude Code/ }).click();
  await add("Claude 自动余额", "claude", "claude-sonnet-4-6");
  await formResult("账户余额 · 7 USD");
  await save();
  await card("Claude 自动余额").getByText("账户余额 · 7 USD", { exact: true }).waitFor();
  await page.reload();
  await page.getByRole("tab", { name: /Claude Code/ }).click();
  await card("Claude 自动余额").getByText("账户余额 · 7 USD", { exact: true }).waitFor();
  checks.push("Claude 供应商表单和列表同样自动查询，重载后无需操作");
  await page.getByRole("tab", { name: /Codex/ }).click();
  subMode = "wallet";
  await refresh("Sub2API 自动识别");
  await audit("供应商自动余额列表");
  await page.getByRole("textbox", { name: "搜索 API 配置" }).fill("自动识别");
  await page.evaluate(() => { document.scrollingElement.scrollTop = 0; });
  await page.screenshot({ path: "docs/screenshots/auto-balance-list.png" });
  await page.getByRole("textbox", { name: "搜索 API 配置" }).fill("");
  await page.setViewportSize({ width: 760, height: 600 });
  await page.getByRole("button", { name: "编辑 New API 自动识别", exact: true }).click();
  await formResult("可用额度 · 100 USD");
  await balanceRegion().scrollIntoViewIfNeeded();
  assert.ok(await page.getByRole("button", { name: "保存配置", exact: true }).isVisible());
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > innerWidth);
  assert.equal(overflow, false);
  await audit("760 像素自动查询表单");
  await page.screenshot({ path: path.join(root, "small-form.png") });
  checks.push("自动表单、供应商列表与最小窗口通过 Axe，无水平溢出");
  assert.deepEqual(errors, []);
  await writeFile(path.join(root, "results.json"), JSON.stringify({ version: "0.3.6", passed: checks.length,
    checks, requests, accessibility, pageErrors: errors, runDirectory }, null, 2));
  console.log(JSON.stringify({ passed: checks.length, checks }, null, 2));
} catch (e) {
  if (page) await page.screenshot({ path: path.join(root, "failure.png") }).catch(() => {});
  throw e;
} finally {
  if (browser) await browser.close();
  if (child && child.exitCode === null) await new Promise((resolve) => {
    child.once("exit", resolve); child.kill();
  });
  await new Promise((resolve) => server.close(resolve));
}
