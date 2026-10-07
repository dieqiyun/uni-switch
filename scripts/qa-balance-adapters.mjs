import { chromium } from "playwright";
import AxeBuilder from "@axe-core/playwright";
import http from "node:http";
import { spawn } from "node:child_process";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import assert from "node:assert/strict";

// Historical 0.3.1 manual-adapter UI checks. Current UI: qa-auto-balance.mjs.
// Build a self-contained QA app with --features qa-webview,tauri/custom-protocol.

const root = path.resolve(".qa/balance-adapters/native");
await mkdir(root, { recursive: true });
await mkdir("docs/screenshots", { recursive: true });
const runId = Date.now().toString();
const codexHome = path.join(root, runId, "Codex 中文");
await mkdir(codexHome, { recursive: true });
const inferenceKey = "sk-isolated-balance-key";
const consoleToken = "isolated-console-access-token";
const requests = [],
  checks = [],
  errors = [],
  accessibility = [];
let subMode = "wallet",
  fail = false,
  unlimitedToken = false;
const server = http.createServer((req, res) => {
  requests.push({
    path: req.url,
    inferenceAuth: req.headers.authorization === `Bearer ${inferenceKey}`,
    consoleAuth: req.headers.authorization === `Bearer ${consoleToken}`,
    userId: req.headers["new-api-user"] || null,
  });
  const send = (value, status = 200) => {
    res.writeHead(status, { "Content-Type": "application/json" });
    res.end(JSON.stringify(value));
  };
  if (fail) return send({ error: "do-not-echo-test-secret" }, 401);
  if (req.url === "/gateway/v1/usage") {
    if (req.headers.authorization !== `Bearer ${inferenceKey}`)
      return send({ error: "wrong key" }, 403);
    if (subMode === "wallet")
      return send({
        mode: "unrestricted",
        isValid: true,
        planName: "钱包余额",
        balance: 23.45,
        remaining: 23.45,
        unit: "USD",
      });
    if (subMode === "zero")
      return send({
        mode: "unrestricted",
        isValid: true,
        planName: "钱包余额",
        balance: 0,
        remaining: 0,
        unit: "USD",
      });
    if (subMode === "unlimited")
      return send({
        mode: "unrestricted",
        isValid: true,
        planName: "无限订阅",
        remaining: -1,
        unit: "USD",
        subscription: {
          daily_limit_usd: null,
          weekly_limit_usd: null,
          monthly_limit_usd: null,
        },
      });
    return send({
      mode: "unrestricted",
      isValid: true,
      planName: "月度套餐",
      remaining: 5,
      unit: "USD",
      subscription: {
        daily_limit_usd: 10,
        daily_usage_usd: 2,
        weekly_limit_usd: 50,
        weekly_usage_usd: 45,
        monthly_limit_usd: 200,
        monthly_usage_usd: 120,
        expires_at: "2026-11-01T00:00:00Z",
      },
    });
  }
  if (req.url === "/gateway/api/usage/token") {
    res.writeHead(307, { Location: "/gateway/api/usage/token/" });
    res.end();
    return;
  }
  if (req.url === "/gateway/api/usage/token/") {
    if (req.headers.authorization !== `Bearer ${inferenceKey}`)
      return send({ success: false }, 403);
    return send({
      code: true,
      message: "ok",
      data: {
        object: "token_usage",
        name: "开发密钥",
        total_available: unlimitedToken ? 0 : 6250000,
        total_used: 1000000,
        total_granted: 7250000,
        unlimited_quota: unlimitedToken,
        expires_at: 0,
      },
    });
  }
  if (req.url === "/gateway/api/user/self") {
    if (
      req.headers.authorization !== `Bearer ${consoleToken}` ||
      req.headers["new-api-user"] !== "42"
    )
      return send({ success: false }, 401);
    return send({
      success: true,
      data: { quota: 4500000, used_quota: 1500000, group: "vip" },
    });
  }
  if (req.url === "/custom") return send({ data: { balance: "0" } });
  send({ error: "not found" }, 404);
});
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const base = `http://127.0.0.1:${server.address().port}/gateway/v1`;
const executable = path.resolve(
  process.env.UNI_SWITCH_QA_EXE ||
    ".qa/balance-adapters/test-app/uni-switch.exe",
);
let child, browser;
const waitUntil = async (fn, timeout = 20000) => {
  const end = Date.now() + timeout;
  while (Date.now() < end) {
    if (await fn()) return;
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error("Timed out waiting for QA desktop");
};
try {
  // The process is owned by this script and uses fresh isolated directories.
  child = spawn(executable, [], {
    windowsHide: true,
    env: {
      ...process.env,
      UNI_SWITCH_DATA_DIR: path.join(root, runId, "data"),
      WEBVIEW2_USER_DATA_FOLDER: path.join(root, runId, "webview"),
      CODEX_HOME: codexHome,
      CLAUDE_CONFIG_DIR: path.join(root, runId, "claude"),
    },
    stdio: "ignore",
  });
  await waitUntil(async () => {
    try {
      return (await fetch("http://127.0.0.1:9223/json/version")).ok;
    } catch {
      return false;
    }
  });
  browser = await chromium.connectOverCDP("http://127.0.0.1:9223");
  const page = browser.contexts()[0].pages()[0];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.getByRole("button", { name: /^添加(第一组)?配置$/ }).waitFor();
  const invoke = (name, args = {}) =>
    page.evaluate(
      ({ name, args }) => window.__TAURI_INTERNALS__.invoke(name, args),
      { name, args },
    );
  const card = (name) =>
    page
      .getByRole("listitem")
      .filter({ has: page.getByRole("heading", { name, exact: true }) });
  const add = async (name, preset) => {
    await page.getByRole("button", { name: /^添加(第一组)?配置$/ }).click();
    await page.locator("#baseUrl").fill(base);
    await page.locator("#apiKey").fill(inferenceKey);
    await page.locator("#model").fill("test-model");
    await page.locator(".form-advanced:not(.balance-options)>summary").click();
    await page.locator("#name").fill(name);
    await page.locator(".balance-options>summary").click();
    await page.locator("#balancePreset").selectOption(preset);
  };
  const query = async () =>
    page.getByRole("button", { name: "查询余额", exact: true }).click();
  const save = async () => {
    await page.getByRole("button", { name: "保存配置", exact: true }).click();
    await page.getByRole("dialog").waitFor({ state: "hidden" });
  };
  const refresh = async (name) => {
    await card(name)
      .getByRole("button", { name: /^(查询|刷新).*余额$/ })
      .click();
    await card(name)
      .getByRole("button", { name: /^(查询|刷新).*余额$/ })
      .waitFor({ state: "visible" });
  };
  const audit = async (name) => {
    await page.evaluate(async () => {
      await Promise.all(
        document
          .getAnimations()
          .filter((a) => a.effect?.getComputedTiming().iterations !== Infinity)
          .map((a) => a.finished.catch(() => {})),
      );
    });
    const result = await new AxeBuilder({ page }).analyze();
    accessibility.push({
      name,
      violations: result.violations.map((v) => ({
        id: v.id,
        impact: v.impact,
        nodes: v.nodes.map((n) => ({
          target: n.target,
          summary: n.failureSummary,
        })),
      })),
    });
    await writeFile(
      path.join(root, "accessibility.json"),
      JSON.stringify(accessibility, null, 2),
    );
    if (result.violations.length)
      await page.screenshot({
        path: path.join(root, "accessibility-failure.png"),
      });
    assert.deepEqual(
      result.violations.map((v) => v.id),
      [],
      `${name}: accessibility`,
    );
  };
  await add("Sub2API 测试", "sub2api");
  assert.equal(await page.locator("#balancePath").count(), 0);
  await query();
  await page.getByText("账户余额 · 23.45 USD", { exact: true }).waitFor();
  await save();
  await refresh("Sub2API 测试");
  await card("Sub2API 测试")
    .getByText("账户余额 · 23.45 USD", { exact: true })
    .waitFor();
  assert.ok(
    requests
      .filter((r) => r.path === "/gateway/v1/usage")
      .every((r) => r.inferenceAuth),
  );
  checks.push("Sub2API 直接使用 API Key，自动保留部署前缀并显示钱包余额");
  subMode = "zero";
  await refresh("Sub2API 测试");
  await card("Sub2API 测试")
    .getByText("账户余额 · 0 USD", { exact: true })
    .waitFor();
  subMode = "subscription";
  await refresh("Sub2API 测试");
  await card("Sub2API 测试")
    .getByText("订阅额度 · 5 USD", { exact: true })
    .waitFor();
  for (const text of ["每日剩余 8 USD", "每周剩余 5 USD", "每月剩余 80 USD"])
    await card("Sub2API 测试").getByText(text, { exact: true }).waitFor();
  await page.screenshot({ path: "docs/screenshots/sub2api-balance.png" });
  subMode = "unlimited";
  await refresh("Sub2API 测试");
  await card("Sub2API 测试")
    .getByText("订阅额度 · 无限额度", { exact: true })
    .waitFor();
  fail = true;
  await refresh("Sub2API 测试");
  await card("Sub2API 测试")
    .getByText(/HTTP 401/)
    .waitFor();
  await card("Sub2API 测试")
    .getByText(/上次结果/)
    .waitFor();
  assert.ok(
    !(await page.locator("body").innerText()).includes(
      "do-not-echo-test-secret",
    ),
  );
  fail = false;
  checks.push("Sub2API 零余额、日周月订阅、无限额度和失败时标记上次结果");

  await add("New API 密钥", "newapi_token");
  await query();
  await page.getByText("密钥额度 · 12.5 USD", { exact: true }).waitFor();
  await page.getByText("已用 2 USD", { exact: true }).waitFor();
  await save();
  unlimitedToken = true;
  await refresh("New API 密钥");
  await card("New API 密钥")
    .getByText("密钥额度 · 无限额度", { exact: true })
    .waitFor();
  assert.ok(
    requests.some(
      (r) =>
        r.path === "/gateway/api/usage/token/" && r.inferenceAuth && !r.userId,
    ),
  );
  assert.ok(!requests.some((r) => r.path === "/gateway/api/usage/token"));
  checks.push("New API 密钥接口保留末尾 /，500000 额度换算，显示无限密钥额度");

  await add("New API 账户", "newapi_account");
  await page.locator("#balanceUserId").fill("42");
  await query();
  await page.getByText(/请填写控制台 Access Token/).waitFor();
  await page.locator("#balanceAccessToken").fill(consoleToken);
  await query();
  await page.getByText("账户余额 · 9 USD", { exact: true }).waitFor();
  await page.getByText("已用 3 USD", { exact: true }).waitFor();
  await audit("New API 账户表单");
  await page
    .locator(".balance-options")
    .screenshot({ path: "docs/screenshots/newapi-balance-settings.png" });
  await save();
  const overview = await invoke("get_overview");
  const account = overview.providers.find((p) => p.name === "New API 账户");
  assert.equal(account.hasBalanceToken, true);
  assert.ok(!JSON.stringify(overview).includes(consoleToken));
  await invoke("apply_provider", { target: "codex", providerId: account.id });
  const applied = await readFile(path.join(codexHome, "config.toml"), "utf8");
  assert.ok(applied.includes(inferenceKey) && !applied.includes(consoleToken));
  await refresh("New API 账户");
  await card("New API 账户")
    .getByText("账户余额 · 9 USD", { exact: true })
    .waitFor();
  assert.ok(
    requests
      .filter((r) => r.path === "/gateway/api/user/self")
      .every((r) => r.consoleAuth && r.userId === "42" && !r.inferenceAuth),
  );
  checks.push(
    "New API 账户使用独立 Access Token 与 New-Api-User；列表摘要和客户端配置不含账户令牌",
  );

  await page.reload();
  await page.getByRole("button", { name: "编辑 New API 账户" }).click();
  assert.equal(
    await page.locator("#balancePreset").inputValue(),
    "newapi_account",
  );
  assert.equal(await page.locator("#balanceAccessToken").inputValue(), "");
  await query();
  await page.getByText("账户余额 · 9 USD", { exact: true }).waitFor();
  await page
    .locator("#balanceSiteUrl")
    .fill(`http://127.0.0.1:${server.address().port}/other`);
  const before = requests.length;
  await query();
  await page.getByText(/更换查询站点后需要重新填写/).waitFor();
  assert.equal(requests.length, before);
  assert.equal(
    await page.getByText("账户余额 · 9 USD", { exact: true }).count(),
    0,
  );
  await page.locator("#balanceSiteUrl").fill("");
  await save();
  await refresh("New API 账户");
  await card("New API 账户")
    .getByText("账户余额 · 9 USD", { exact: true })
    .waitFor();
  checks.push("编辑保留类型和隐藏令牌，空值保留；更换站点时先拒绝复用旧令牌");

  await add("自定义兼容", "custom");
  await page.locator("#balancePath").fill("/custom");
  await page.locator("#balanceJsonPath").fill("data.balance");
  await page.locator("#balanceUnit").fill("USD");
  await query();
  await page.getByText("余额 · 0 USD", { exact: true }).waitFor();
  await save();
  checks.push("原自定义 JSON 查询继续支持数字字符串和零余额");
  // Restore useful values for the final view.
  subMode = "subscription";
  unlimitedToken = false;
  for (const name of ["Sub2API 测试", "New API 密钥", "New API 账户"])
    await refresh(name);
  await page.waitForTimeout(250);
  await audit("余额供应商列表");
  await page.screenshot({ path: "docs/screenshots/balance-adapters-list.png" });
  await page.setViewportSize({ width: 760, height: 600 });
  await page.getByRole("button", { name: "编辑 New API 账户" }).click();
  await page.locator("#balanceUserId").scrollIntoViewIfNeeded();
  assert.ok(
    await page
      .getByRole("button", { name: "保存配置", exact: true })
      .isVisible(),
  );
  await audit("760 像素账户表单");
  await page.screenshot({ path: path.join(root, "small-form.png") });
  await page.getByRole("button", { name: "取消", exact: true }).click();
  checks.push("列表、账户表单及最小窗口布局，Axe 检查无违规");
  assert.deepEqual(errors, []);
  await writeFile(
    path.join(root, "results.json"),
    JSON.stringify(
      {
        version: "0.3.1",
        passed: checks.length,
        checks,
        requests,
        accessibility,
        pageErrors: errors,
        runDirectory: path.join(root, runId),
      },
      null,
      2,
    ),
  );
  console.log(JSON.stringify({ passed: checks.length, checks }, null, 2));
} finally {
  if (browser) await browser.close();
  if (child && child.exitCode === null) {
    await new Promise((resolve) => {
      child.once("exit", resolve);
      child.kill();
    });
  }
  await new Promise((resolve) => server.close(resolve));
}
