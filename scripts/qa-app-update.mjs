import assert from "node:assert/strict";
import path from "node:path";
import { readFile } from "node:fs/promises";

export async function verifyAppUpdates({
  page,
  invoke,
  snapshot,
  root,
  version,
  checks,
  audit,
  mock,
}) {
  const before = await snapshot();
  const requests = mock.requests;
  assert.equal(requests.length, 1, "Startup performs one update check");
  assert.ok(requests.every((request) => !request.auth && !request.key));
  const entry = page.locator(".sidebar .app-update-entry");
  await entry.getByText("有新版本", { exact: true }).waitFor();
  assert.equal(await entry.getByText(`v${version}`, { exact: true }).count(), 1);
  assert.equal(await page.locator(".workspace-footer .update-entry").count(), 0);
  assert.equal(await page.locator(".workspace-footer").getByText(`v${version}`, { exact: true }).count(), 0);
  await audit("Sidebar update entry available");
  await page.screenshot({ path: `docs/screenshots/sidebar-update-available-${version}.png` });
  assert.equal(await page.getByRole("dialog").count(), 0);
  await entry.focus();
  await page.keyboard.press("Enter");
  let dialog = page.getByRole("dialog", { name: "软件更新", exact: true });
  await dialog.getByRole("button", { name: "检测更新", exact: true }).waitFor();
  assert.equal(requests.length, 2, "Opening the version entry performs a fresh check");
  await dialog.getByText("新版本 v9.0.0", { exact: true }).waitFor();
  await audit("GitHub update available");
  await page.screenshot({
    path: `docs/screenshots/github-update-${version}.png`,
  });
  await dialog.getByRole("button", { name: "前往下载", exact: true }).click();
  assert.equal(
    await readFile(path.join(root, "opened-release.txt"), "utf8"),
    "https://github.com/example/uni-switch/releases/tag/v9.0.0",
  );
  await assert.rejects(
    invoke("open_app_release", {
      url: "https://github.com/other/repo/releases/tag/v9.0.0",
    }),
    (error) => error.code === "update_url",
  );
  checks.push(
    "启动只检测一次；左下角版本入口可用键盘打开并立即刷新，发现新版本显示在同一入口，下载打开固定发布仓库且不使用供应商认证",
  );
  mock.mode = "rate";
  await dialog.getByRole("button", { name: "检测更新", exact: true }).click();
  await dialog.getByRole("alert").waitFor();
  assert.equal(
    await dialog.getByText("新版本 v9.0.0", { exact: true }).count(),
    1,
  );
  assert.equal(
    await dialog.getByText("已是最新版本", { exact: true }).count(),
    0,
  );
  await audit("GitHub rate limit error");
  mock.mode = "current";
  await dialog.getByRole("button", { name: "检测更新", exact: true }).click();
  await dialog.getByText("已是最新版本", { exact: true }).waitFor();
  assert.equal(await entry.getByText("有新版本", { exact: true }).count(), 0);
  assert.equal(await entry.getByText("检查更新", { exact: true }).count(), 1);
  mock.mode = "missing";
  await dialog.getByRole("button", { name: "检测更新", exact: true }).click();
  await dialog.getByRole("alert").waitFor();
  assert.equal(
    await dialog.getByText("已是最新版本", { exact: true }).count(),
    0,
  );
  mock.mode = "old";
  await dialog.getByRole("button", { name: "检测更新", exact: true }).click();
  await dialog.getByText("已是最新版本", { exact: true }).waitFor();
  await page.keyboard.press("Escape");
  await dialog.waitFor({ state: "hidden" });
  assert.equal(await entry.evaluate((el) => el === document.activeElement), true);
  await audit("Sidebar update entry current");
  await page.screenshot({ path: `docs/screenshots/sidebar-update-${version}.png` });
  await page.getByRole("button", { name: "设置", exact: true }).click();
  dialog = page.getByRole("dialog", { name: "Codex 设置", exact: true });
  assert.equal(await dialog.getByRole("heading", { name: "软件更新", exact: true }).count(), 0);
  await page.getByRole("button", { name: "关闭弹窗", exact: true }).click();
  mock.mode = "new";
  await entry.click();
  dialog = page.getByRole("dialog", { name: "软件更新", exact: true });
  await dialog.getByText("新版本 v9.0.0", { exact: true }).waitFor();
  await page.setViewportSize({ width: 390, height: 620 });
  assert.equal(
    await dialog.evaluate((el) => el.scrollWidth > el.clientWidth),
    false,
  );
  await audit("Update dialog narrow window");
  await page.getByRole("button", { name: "关闭弹窗", exact: true }).click();
  await entry.waitFor({ state: "visible" });
  assert.equal(await entry.evaluate((el) => el === document.activeElement), true);
  assert.equal(await page.locator(".app-shell").evaluate((el) => el.scrollWidth > el.clientWidth), false);
  await audit("Sidebar update entry narrow window");
  await entry.click();
  await page.getByRole("dialog", { name: "软件更新", exact: true }).waitFor();
  await page.keyboard.press("Escape");
  assert.equal(await page.getByRole("dialog").count(), 0);
  assert.deepEqual(await snapshot(), before);
  assert.ok(requests.every((request) => !request.auth && !request.key));
  checks.push(
    "更新可手动重试；相同及旧版本仍保留左下角入口，关闭弹窗返回入口，移除设置与页脚重复入口，窄窗可用且不修改客户端配置",
  );
  await page.setViewportSize({ width: 1120, height: 780 });
}
