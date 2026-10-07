import assert from "node:assert/strict";
import path from "node:path";
import { readFile } from "node:fs/promises";

export async function verifyTutorial({ page, snapshot, root, checks, audit }) {
  const before = await snapshot();
  assert.equal(
    await page
      .getByRole("button", { name: "导入现有配置", exact: true })
      .count(),
    0,
  );
  const command = await page.evaluate(async () => {
    try {
      await window.__TAURI_INTERNALS__.invoke("import_current", {
        target: "codex",
      });
      return "accepted";
    } catch (e) {
      return String(e?.message || e);
    }
  });
  assert.match(command, /import_current.*not found|not found.*import_current/i);
  const entry = page.getByRole("button", { name: "使用说明", exact: true });
  await entry.click();
  const dialog = page.getByRole("dialog", { name: "使用说明", exact: true });
  const article = dialog.getByRole("article");
  const nav = dialog.getByRole("navigation", { name: "教程目录", exact: true });
  const topics = JSON.parse(
    await readFile("src/content/tutorial.json", "utf8"),
  ).topics;
  for (const topic of topics) {
    const button = nav.getByRole("button", { name: topic.title, exact: true });
    await button.click();
    assert.equal(await button.getAttribute("aria-current"), "page");
    await article
      .getByRole("heading", { name: topic.title, exact: true })
      .waitFor();
    for (const section of topic.sections)
      assert.equal(
        await article
          .getByRole("heading", { name: section.title, exact: true })
          .count(),
        1,
      );
    await audit(`Offline tutorial ${topic.id}`);
  }
  assert.equal(
    await article.getByText(/存在冲突时不会强制覆盖文件/).count(),
    1,
  );
  await page.screenshot({
    path: "docs/screenshots/tutorial-troubleshooting.png",
  });
  await nav.getByRole("button", { name: "首次配置", exact: true }).click();
  await page.screenshot({ path: "docs/screenshots/tutorial-start.png" });
  const link = dialog.getByRole("link", {
    name: "GitHub 完整教程",
    exact: true,
  });
  const url =
    "https://github.com/dieqiyun/uni-switch/blob/main/docs/tutorial.md";
  assert.equal(await link.getAttribute("href"), url);
  await link.click();
  const until = Date.now() + 10000;
  while (
    (await readFile(path.join(root, "opened-project.txt"), "utf8").catch(
      () => "",
    )) !== url
  ) {
    if (Date.now() > until) throw new Error("Tutorial link marker timed out");
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  for (const size of [
    { width: 560, height: 520 },
    { width: 390, height: 620 },
    { width: 800, height: 420 },
  ]) {
    await page.setViewportSize(size);
    assert.equal(
      await dialog.evaluate((el) => el.scrollWidth > el.clientWidth),
      false,
    );
    assert.equal(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
      false,
    );
    for (const label of ["GitHub 完整教程", "开始使用"]) {
      const rect = await dialog
        .getByRole(label === "开始使用" ? "button" : "link", {
          name: label,
          exact: true,
        })
        .boundingBox();
      assert.ok(
        rect.x >= 0 &&
          rect.y >= 0 &&
          rect.y + rect.height <= size.height &&
          rect.x + rect.width <= size.width,
      );
    }
    assert.equal(
      await article.evaluate((el) => el.scrollHeight > el.clientHeight),
      true,
    );
    await audit(`Offline tutorial ${size.width}x${size.height}`);
    if (size.width === 390)
      await page.screenshot({ path: "docs/screenshots/tutorial-narrow.png" });
  }
  await page.keyboard.press("Escape");
  await dialog.waitFor({ state: "hidden" });
  assert.equal(
    await entry.evaluate((el) => el === document.activeElement),
    true,
  );
  await page.setViewportSize({ width: 1120, height: 780 });
  await entry.click();
  await dialog.getByRole("button", { name: "开始使用", exact: true }).click();
  assert.equal(await dialog.count(), 0);
  assert.deepEqual(await snapshot(), before);
  checks.push(
    "导入入口和原生import_current命令彻底移除；8类离线教程、固定GitHub教程跳转、窄窗/低高度/键盘返回焦点均可用且不改变数据库和客户端文件",
  );
}
