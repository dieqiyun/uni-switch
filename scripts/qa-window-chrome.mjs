import { spawn } from "node:child_process";
import assert from "node:assert/strict";
import { writeFile } from "node:fs/promises";

export async function verifyWindowChrome({ page, child, invoke, checks }) {
  const native = (action = "state") =>
    new Promise((resolve, reject) => {
      const probe = spawn(
        "powershell.exe",
        [
          "-NoProfile",
          "-ExecutionPolicy",
          "Bypass",
          "-File",
          "scripts/qa-window-native.ps1",
          "-TestProcessId",
          String(child.pid),
          "-Action",
          action,
        ],
        { windowsHide: true },
      );
      let output = "",
        errors = "";
      probe.stdout.on("data", (part) => (output += part));
      probe.stderr.on("data", (part) => (errors += part));
      probe.once("error", reject);
      probe.once("exit", (code) => {
        if (code !== 0) return reject(new Error(errors));
        try {
          resolve(JSON.parse(output.trim()));
        } catch (error) {
          reject(error);
        }
      });
    });
  const state = () => invoke("plugin:window|is_maximized");
  assert.equal(await invoke("plugin:window|is_decorated"), false);
  assert.equal(await invoke("plugin:window|is_resizable"), true);
  const original = await native();
  // Windows keeps WS_CAPTION for its shadow/resize machinery; the actual
  // client origin verifies that no native title bar occupies the top.
  assert.ok(
    original.nonClientTop <= 2,
    `Native top inset: ${original.nonClientTop}`,
  );
  assert.equal(original.resizeFrame, true);
  checks.push("无原生标题栏，保留系统阴影与可调整大小的窗口");

  await page.getByRole("button", { name: "最大化窗口", exact: true }).click();
  await page.getByRole("button", { name: "还原窗口", exact: true }).waitFor();
  assert.equal(await state(), true);
  await page.getByRole("button", { name: "还原窗口", exact: true }).click();
  await page.getByRole("button", { name: "最大化窗口", exact: true }).waitFor();
  assert.equal(await state(), false);
  await page
    .locator(".window-drag-region")
    .dblclick({ position: { x: 400, y: 16 } });
  await page.getByRole("button", { name: "还原窗口", exact: true }).waitFor();
  assert.equal(await state(), true);
  await page
    .locator(".window-drag-region")
    .dblclick({ position: { x: 400, y: 16 } });
  await page.getByRole("button", { name: "最大化窗口", exact: true }).waitFor();
  checks.push("自定义最大化/还原按钮和顶部双击均驱动真实窗口，状态标签同步");

  const resized = await native("resize");
  let dragged = null;
  const skipped = [];
  try {
    dragged = await native("drag");
    assert.ok(Math.abs(dragged.x - resized.x) >= 35);
    assert.ok(Math.abs(dragged.y - resized.y) >= 15);
    checks.push("真实鼠标拖动无框窗口成功，窗口缩放后功能按钮可用");
  } catch (error) {
    if (
      !String(error.message).includes("QA window could not receive mouse input")
    )
      throw error;
    skipped.push({
      check: "native_mouse_drag",
      reason: "Windows denied foreground mouse input to the isolated QA window",
    });
    console.log(
      "QA native mouse drag not run: Windows denied foreground input",
    );
  }

  await page.getByRole("button", { name: "最小化窗口" }).click();
  assert.equal((await native()).minimized, true);
  await native("restore");
  assert.equal((await native()).minimized, false);
  await page.getByRole("button", { name: "关闭窗口并留在托盘" }).click();
  assert.equal((await native()).visible, false);
  assert.equal(child.exitCode, null);
  await native("restore");
  assert.equal((await native()).visible, true);
  await page.getByRole("button", { name: "设置", exact: true }).click();
  await page.getByRole("dialog").waitFor();
  await page.keyboard.press("Escape");
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  checks.push("真实最小化/恢复与关闭隐藏后保留进程，恢复后设置及弹窗正常");
  await page.evaluate(() => document.activeElement?.blur());
  await page.screenshot({
    path: "docs/screenshots/frameless-window-0.5.16.png",
  });
  await writeFile(
    ".qa/ux-flow/window-chrome-results.json",
    JSON.stringify(
      {
        version: "0.5.16",
        nativeOriginal: original,
        resized,
        dragged,
        skipped,
        checks: checks.slice(-4),
      },
      null,
      2,
    ),
  );
  return skipped;
}
