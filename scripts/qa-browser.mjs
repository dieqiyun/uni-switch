import { chromium } from "playwright";
import AxeBuilder from "@axe-core/playwright";
import { mkdir, writeFile } from "node:fs/promises";
import assert from "node:assert/strict";
await mkdir(".qa/codex-options/regression", { recursive: true });
const browser = await chromium.launch({ channel: "chrome", headless: true });
const context = await browser.newContext({
  viewport: { width: 1120, height: 780 },
});
const page = await context.newPage();
const errors = [];
page.on("pageerror", (error) => errors.push(error.message));
const checks = [];
const accessibility = [];
const settle = async () =>
  page.evaluate(async () => {
    await Promise.all(
      document
        .getAnimations()
        .filter(
          (animation) =>
            animation.effect?.getComputedTiming().iterations !== Infinity,
        )
        .map((animation) => animation.finished.catch(() => {})),
    );
  });
const audit = async (name) => {
  await settle();
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
};
const more = async () => {
  await page.locator(".form-advanced:not(.balance-options)>summary").click();
};
const create = async (name, model, url, key, apply = false) => {
  const add = page.getByRole("button", { name: "添加配置", exact: true });
  if (await add.count()) await add.click();
  else await page.getByRole("button", { name: "添加第一组配置" }).click();
  await page.locator("#baseUrl").fill(url);
  await page.locator("#apiKey").fill(key);
  await page.locator("#model").fill(model);
  await more();
  await page.locator("#name").fill(name);
  await page
    .getByRole("button", {
      name: apply ? "保存并应用" : "保存配置",
      exact: true,
    })
    .click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
};
try {
  await page.goto("http://127.0.0.1:1420");
  await page.getByRole("button", { name: "添加第一组配置" }).waitFor();
  await page.waitForFunction(
    () =>
      [...document.querySelectorAll(".app-nav img")].length === 2 &&
      [...document.querySelectorAll(".app-nav img")].every(
        (image) => image.complete && image.naturalWidth > 0,
      ),
  );
  assert.equal(await page.locator(".connection-aside").count(), 0);
  await page.screenshot({ path: ".qa/codex-options/regression/empty.png" });
  await audit("首次使用页面");
  await page.getByRole("button", { name: "添加第一组配置" }).click();
  assert.equal(
    await page
      .locator("#baseUrl")
      .evaluate((n) => n === document.activeElement),
    true,
  );
  assert.equal(await page.locator(".form-advanced:not(.balance-options)").getAttribute("open"), null);
  await settle();
  await page.screenshot({ path: ".qa/codex-options/regression/form.png" });
  await audit("默认三项表单");
  await page.getByRole("button", { name: "保存并应用" }).click();
  await page.getByText("请填写 API 地址", { exact: true }).waitFor();
  assert.equal(
    await page.locator("#baseUrl").getAttribute("aria-invalid"),
    "true",
  );
  checks.push("首次使用引导、三项表单、自动名称、输入焦点和空表单校验");
  await page.locator("#baseUrl").fill("https://codex.example.test/v1");
  await page.locator("#apiKey").fill("test-key-1234");
  await page.locator("#model").fill("codex-test-model");
  await more();
  await page.locator("#name").fill("工作 Codex");
  await page.getByRole("button", { name: "保存配置", exact: true }).click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  assert.equal(
    await page
      .getByRole("button", { name: "添加配置", exact: true })
      .evaluate((n) => n === document.activeElement),
    true,
  );
  assert.equal(await page.getByText("当前使用", { exact: true }).count(), 0);
  await page.getByRole("button", { name: "应用", exact: true }).click();
  await page.getByRole("button", { name: "已应用", exact: true }).waitFor();
  assert.equal(
    await page
      .getByRole("button", { name: "已应用", exact: true })
      .isDisabled(),
    true,
  );
  assert.equal(await page.getByText("当前连接", { exact: true }).count(), 0);
  checks.push("仅保存不应用、首个配置创建后焦点返回、已应用状态与下一步提示");
  await page.getByRole("button", { name: "编辑 工作 Codex" }).click();
  assert.equal(await page.locator("#apiKey").inputValue(), "");
  await page.locator("#model").fill("updated-model");
  await page.getByRole("button", { name: "保存配置", exact: true }).click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  await page.getByText("有待应用的修改", { exact: true }).waitFor();
  await page.getByRole("button", { name: "重新应用", exact: true }).click();
  await page.getByRole("button", { name: "已应用", exact: true }).waitFor();
  checks.push("编辑保留密钥，修改后明确重新应用");
  await create(
    "备用 Codex",
    "backup-model",
    "https://backup.example.test/v1",
    "test-backup-key",
  );
  await page.getByRole("tab", { name: /Claude Code/ }).click();
  assert.equal(
    await page
      .getByRole("button", { name: "桌面端", exact: true })
      .getAttribute("aria-pressed"),
    "true",
  );
  await page.getByRole("button", { name: "添加第一组配置" }).click();
  await page.locator("#baseUrl").fill("https://claude.example.test");
  await page.locator("#apiKey").fill("test-claude-5678");
  await page.locator("#model").fill("claude-sonnet-4-6");
  await more();
  await page.locator("#name").fill("Claude 工作网关");
  await page.locator("#authMode").selectOption("x-api-key");
  await page.getByRole("button", { name: "显示 API Key" }).click();
  assert.equal(await page.locator("#apiKey").getAttribute("type"), "text");
  await page.getByRole("button", { name: "隐藏 API Key" }).click();
  await audit("更多选项与认证选择");
  await page.getByRole("button", { name: "保存并应用", exact: true }).click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  await page.getByRole("button", { name: "已应用", exact: true }).waitFor();
  checks.push("Claude 默认桌面、认证选择、密钥隐藏、保存并应用一键完成");
  await page.getByRole("button", { name: "关闭提示" }).click();
  await page.evaluate(() => document.activeElement?.blur());
  await page.screenshot({ path: ".qa/codex-options/regression/claude-desktop.png" });
  await audit("已应用配置页面");
  await page.getByRole("button", { name: "CLI", exact: true }).click();
  assert.equal(await page.getByText("当前使用", { exact: true }).count(), 0);
  await page.getByRole("button", { name: "应用", exact: true }).click();
  await page.getByRole("button", { name: "已应用", exact: true }).waitFor();
  await page.getByRole("button", { name: "设置", exact: true }).click();
  await page.getByRole("button", { name: "恢复原配置", exact: true }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "恢复原配置", exact: true })
    .click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  checks.push("桌面与 CLI 独立状态，恢复确认和操作反馈");
  await page.getByRole("button", { name: "添加配置", exact: true }).click();
  await page.keyboard.press("Escape");
  assert.equal(await page.getByRole("dialog").count(), 0);
  assert.equal(
    await page
      .getByRole("button", { name: "添加配置", exact: true })
      .evaluate((n) => n === document.activeElement),
    true,
  );
  await page.getByRole("button", { name: "设置", exact: true }).click();
  await page.getByRole("button", { name: "修改目录", exact: true }).click();
  await page.locator("#directory").fill("D:\\演示 配置");
  await page.getByRole("button", { name: "保存目录" }).click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  await page.getByRole("button", { name: "设置", exact: true }).click();
  await page.getByText("D:\\演示 配置", { exact: true }).waitFor();
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "导入现有配置" }).click();
  await page.getByText("请在桌面应用中导入客户端的现有配置。").waitFor();
  await page.getByRole("button", { name: "使用说明", exact: true }).click();
  await page.getByRole("dialog").waitFor();
  await page.getByRole("button", { name: "开始使用" }).click();
  checks.push("键盘 Escape、焦点返回、设置中的目录和恢复、导入错误和使用说明");
  await page.getByRole("tab", { name: /Codex/ }).click();
  await page.evaluate(() => document.activeElement?.blur());
  await page.screenshot({ path: ".qa/codex-options/regression/codex-list.png" });
  for (const size of [
    { width: 1440, height: 900 },
    { width: 1120, height: 780 },
    { width: 760, height: 600 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(size);
    assert.equal(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
      false,
    );
    await page.screenshot({
      path: `.qa/codex-options/regression/viewport-${size.width}.png`,
      fullPage: true,
    });
    await page.getByRole("button", { name: "添加配置", exact: true }).click();
    assert.equal(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
      false,
    );
    await page.screenshot({ path: `.qa/codex-options/regression/form-${size.width}.png` });
    await page.keyboard.press("Escape");
  }
  checks.push("1440、1120、760、390 窗口页面及表单无横向溢出");
  await page.setViewportSize({ width: 1120, height: 780 });
  for (let n = 0; n < 3; n++)
    await create(
      `更多配置 ${n + 1}`,
      "test-model",
      `https://api${n}.example.test/v1`,
      "test-key",
    );
  await page
    .getByRole("textbox", { name: "搜索 API 配置" })
    .fill("没有这个配置");
  await page.getByText("没有找到匹配的配置", { exact: true }).waitFor();
  await page.getByRole("button", { name: "清空搜索" }).click();
  assert.equal(
    await page
      .getByRole("listitem")
      .filter({ has: page.locator(".provider-name") })
      .count(),
    5,
  );
  checks.push("多个配置的搜索与清空搜索");
  assert.deepEqual(errors, []);
  await writeFile(
    ".qa/codex-options/regression/browser-results.json",
    JSON.stringify(
      { passed: checks.length, checks, pageErrors: errors, accessibility },
      null,
      2,
    ),
  );
  console.log(
    JSON.stringify({ passed: checks.length, checks, accessibility }, null, 2),
  );
  assert.equal(
    accessibility.flatMap((a) => a.violations).length,
    0,
    "页面和表单应通过 Axe 检查",
  );
} finally {
  await browser.close();
}
