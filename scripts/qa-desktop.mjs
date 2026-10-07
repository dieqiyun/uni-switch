import { chromium } from "playwright";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import assert from "node:assert/strict";

const root = path.resolve(".qa/codex-options/regression/native-clients");
const codexDir = path.join(root, "Codex 中文");
const desktopDir = path.join(root, "Desktop");
const cliDir = path.join(root, "Claude CLI");
const put = async (file, data) => {
  await mkdir(path.dirname(file), { recursive: true });
  await writeFile(file, data);
};
const load = async (file) => JSON.parse(await readFile(file, "utf8"));
await put(
  path.join(codexDir, "config.toml"),
  '# 保留注释\nmodel = "original-model"\n[mcp_servers.example]\ncommand = "demo"\n',
);
await put(
  path.join(desktopDir, "Claude/claude_desktop_config.json"),
  JSON.stringify({
    deploymentMode: "1p",
    mcpServers: { example: { command: "demo" } },
  }),
);
await put(
  path.join(desktopDir, "Claude-3p/claude_desktop_config.json"),
  JSON.stringify({ custom: true }),
);
await put(
  path.join(cliDir, "settings.json"),
  JSON.stringify({ env: { KEEP: "preserved" }, hooks: { Stop: [] } }),
);
const browser = await chromium.connectOverCDP("http://127.0.0.1:9223");
const pages = browser.contexts().flatMap((context) => context.pages());
const page =
  pages.find(
    (page) => page.url().includes("tauri") || page.url().includes("1420"),
  ) || pages[0];
assert.ok(page, "Tauri WebView 页面应可访问");
const errors = [];
page.on("pageerror", (error) => errors.push(error.message));
const checks = [];
const setDirectory = async (directory) => {
  await page.getByRole("button", { name: "设置", exact: true }).click();
  await page.getByRole("button", { name: "修改目录", exact: true }).click();
  await page.locator("#directory").fill(directory);
  await page.getByRole("button", { name: "保存目录" }).click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
};
const create = async (name, model, baseUrl, key) => {
  const add = page.getByRole("button", { name: "添加配置", exact: true });
  if (await add.count()) await add.click();
  else await page.getByRole("button", { name: "添加第一组配置" }).click();
  await page.locator(".form-advanced:not(.balance-options) > summary").click();
  await page.locator("#name").fill(name);
  await page.locator("#baseUrl").fill(baseUrl);
  await page.locator("#apiKey").fill(key);
  await page.locator("#model").fill(model);
  await page.getByRole("button", { name: "保存配置" }).click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
};
const card = (name) =>
  page
    .getByRole("listitem")
    .filter({ has: page.getByRole("heading", { name, exact: true }) });
const apply = async (name) => {
  await card(name)
    .getByRole("button", { name: /^(应用|重新应用)$/ })
    .click();
  await card(name)
    .getByRole("button", { name: "已应用", exact: true })
    .waitFor();
};
const restore = async () => {
  await page.getByRole("button", { name: "设置", exact: true }).click();
  await page.getByRole("button", { name: "恢复原配置", exact: true }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "恢复原配置", exact: true })
    .click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
};

try {
  await page.getByRole("button", { name: /^添加(第一组)?配置$/ }).waitFor();
  assert.equal(await page.getByText(/浏览器预览/).count(), 0);
  await page.screenshot({ path: ".qa/native-empty.png" });
  await setDirectory(codexDir);
  await create(
    "Codex 测试 API",
    "test-model",
    "https://codex.example.test/v1",
    "desktop-test-key-1234",
  );
  await apply("Codex 测试 API");
  const codex = await readFile(path.join(codexDir, "config.toml"), "utf8");
  assert.ok(
    codex.includes('experimental_bearer_token = "desktop-test-key-1234"'),
  );
  assert.ok(
    codex.includes("# 保留注释") && codex.includes("mcp_servers.example"),
  );
  checks.push("真实 Tauri IPC：Codex 地址、Key、模型写入和原注释保留");
  await page.getByRole("button", { name: "导入现有配置" }).click();
  await page
    .getByRole("heading", { name: "导入的配置", exact: true })
    .waitFor();
  await page.getByRole("button", { name: "删除 Codex 测试 API" }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "删除配置" })
    .click();
  await page
    .getByRole("dialog")
    .getByText(/此配置仍在客户端中使用/)
    .waitFor();
  await page.getByRole("dialog").getByRole("button", { name: "取消" }).click();
  checks.push("导入现有 Codex 与删除使用中配置的错误反馈");
  await restore();
  const restoredCodex = await readFile(
    path.join(codexDir, "config.toml"),
    "utf8",
  );
  assert.ok(
    restoredCodex.includes("original-model") &&
      !restoredCodex.includes("desktop-test-key"),
  );
  await page.getByRole("button", { name: "删除 导入的配置" }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "删除配置" })
    .click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  checks.push("恢复 Codex 原模型和删除未使用配置");
  await page.getByRole("tab", { name: /Claude Code/ }).click();
  await setDirectory(desktopDir);
  await create(
    "Claude 桌面测试",
    "claude-sonnet-4-6",
    "https://claude.example.test",
    "desktop-claude-test-5678",
  );
  await apply("Claude 桌面测试");
  const profileId = "e82de475-47fa-4c54-9000-13571c000001";
  const normalPath = path.join(desktopDir, "Claude/claude_desktop_config.json");
  const profilePath = path.join(
    desktopDir,
    `Claude-3p/configLibrary/${profileId}.json`,
  );
  assert.equal((await load(normalPath)).deploymentMode, "3p");
  assert.ok((await load(normalPath)).mcpServers.example);
  const profile = await load(profilePath);
  assert.equal(profile.inferenceProvider, "gateway");
  assert.equal(profile.inferenceGatewayApiKey, "desktop-claude-test-5678");
  assert.equal(
    (await load(path.join(desktopDir, "Claude-3p/configLibrary/_meta.json")))
      .appliedId,
    profileId,
  );
  checks.push("Claude 桌面端四文件：部署模式、profile、元数据与 MCP 保留");
  await page.screenshot({ path: ".qa/native-claude-desktop.png" });
  await page.getByRole("button", { name: "编辑 Claude 桌面测试" }).click();
  await page.locator(".form-advanced:not(.balance-options) > summary").click();
  await page.locator("#authMode").selectOption("x-api-key");
  await page.getByRole("button", { name: "保存配置" }).click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  await page.getByText("有待应用的修改", { exact: true }).waitFor();
  await apply("Claude 桌面测试");
  assert.equal(
    (await load(profilePath)).inferenceGatewayAuthScheme,
    "x-api-key",
  );
  checks.push("编辑密钥留空保留、待应用状态、桌面认证模式切换");
  await page.getByRole("button", { name: "CLI", exact: true }).click();
  await setDirectory(cliDir);
  await apply("Claude 桌面测试");
  const cliPath = path.join(cliDir, "settings.json");
  const cli = await load(cliPath);
  assert.equal(cli.env.ANTHROPIC_API_KEY, "desktop-claude-test-5678");
  assert.equal(cli.env.KEEP, "preserved");
  assert.ok(cli.hooks);
  await restore();
  assert.deepEqual(await load(cliPath), {
    env: { KEEP: "preserved" },
    hooks: { Stop: [] },
  });
  checks.push("CLI 独立写入、认证变量、hooks 保留和恢复");
  await put(cliPath, "{ broken");
  await card("Claude 桌面测试")
    .getByRole("button", { name: "应用", exact: true })
    .click();
  await page
    .getByRole("alert")
    .filter({ hasText: /JSON 无效/ })
    .waitFor();
  assert.equal(await readFile(cliPath, "utf8"), "{ broken");
  await page.getByRole("button", { name: "添加配置", exact: true }).click();
  await page.locator("#baseUrl").fill("https://retry.example.test");
  await page.locator("#apiKey").fill("retry-test-key");
  await page.locator("#model").fill("claude-sonnet-4-6");
  await page.locator(".form-advanced:not(.balance-options) > summary").click();
  await page.locator("#name").fill("CLI 重试测试");
  await page.getByRole("button", { name: "保存并应用", exact: true }).click();
  await page
    .getByRole("dialog")
    .getByText(/配置已保存，应用未完成/)
    .waitFor();
  assert.equal(await readFile(cliPath, "utf8"), "{ broken");
  await put(cliPath, "{}");
  await page.getByRole("button", { name: "保存并应用", exact: true }).click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  assert.equal(
    (await load(cliPath)).env.ANTHROPIC_AUTH_TOKEN,
    "retry-test-key",
  );
  assert.equal(
    await page
      .getByRole("heading", { name: "CLI 重试测试", exact: true })
      .count(),
    1,
  );
  await restore();
  await page.getByRole("button", { name: "删除 CLI 重试测试" }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "删除配置" })
    .click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  checks.push("损坏 JSON 保留原文件，保存并应用失败后可重试且不重复新增");
  await page.getByRole("button", { name: "桌面端", exact: true }).click();
  await restore();
  assert.equal((await load(normalPath)).deploymentMode, "1p");
  assert.ok((await load(normalPath)).mcpServers.example);
  checks.push("桌面恢复部署模式并保留原 MCP");
  await apply("Claude 桌面测试");
  await page.evaluate(() => scrollTo(0, 0));
  await page.getByRole("list", { name: "已保存的 API 配置" }).waitFor();
  await page.waitForFunction(
    () =>
      document.querySelector(".provider-list")?.getAttribute("aria-busy") ===
      "false",
  );
  await page.evaluate(() => document.activeElement?.blur());
  await page.screenshot({ path: ".qa/codex-options/regression/native-final.png" });
  assert.deepEqual(errors, []);
  await writeFile(
    ".qa/codex-options/regression/desktop-results.json",
    JSON.stringify(
      { passed: checks.length, checks, pageErrors: errors },
      null,
      2,
    ),
  );
  console.log(JSON.stringify({ passed: checks.length, checks }, null, 2));
} finally {
  await browser.close();
}
