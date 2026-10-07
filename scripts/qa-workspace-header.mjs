import assert from "node:assert/strict";

export async function verifyWorkspaceHeader({ page, checks, audit, version }) {
  const header = page.locator(".workspace-header");
  const checkLayout = async (singleRow) => {
    assert.equal(
      await page.locator(".destination-bar, .list-toolbar").count(),
      0,
    );
    assert.equal(await page.getByText("应用到", { exact: true }).count(), 0);
    assert.equal(
      await page
        .getByText("使用同一目录时，一次应用即可", { exact: true })
        .count(),
      0,
    );
    const geometry = await header.evaluate((el) => {
      const bounds = el.getBoundingClientRect();
      const title = el.querySelector("h1").getBoundingClientRect();
      return {
        bounds: { x: bounds.x, right: bounds.right, bottom: bounds.bottom },
        titleCenter: title.y + title.height / 2,
        buttons: [...el.querySelectorAll("button")].map((button) => {
          const rect = button.getBoundingClientRect();
          return {
            x: rect.x,
            right: rect.right,
            y: rect.y,
            bottom: rect.bottom,
            center: rect.y + rect.height / 2,
          };
        }),
      };
    });
    for (const button of geometry.buttons) {
      assert.ok(
        button.x >= geometry.bounds.x && button.right <= geometry.bounds.right,
        "Every header button fits inside the workspace",
      );
      assert.ok(
        button.y >= 0 && button.bottom <= geometry.bounds.bottom,
        "Header buttons stay visible",
      );
      if (singleRow)
        assert.ok(
          Math.abs(button.center - geometry.titleCenter) < 1,
          "Title and actions share one row",
        );
    }
    assert.equal(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
      false,
    );
  };
  await page.setViewportSize({ width: 1120, height: 780 });
  assert.equal(
    await header.getByRole("heading", { name: "Codex", exact: true }).count(),
    1,
  );
  await checkLayout(true);
  await audit("Merged Codex toolbar");
  await page.screenshot({
    path: `docs/screenshots/compact-codex-header-${version}.png`,
  });
  await header.getByRole("button", { name: "搜索供应商", exact: true }).click();
  await page.getByRole("textbox", { name: "搜索 API 配置" }).fill("导入");
  assert.equal(await page.locator(".provider-card").count(), 1);
  await page.getByRole("textbox", { name: "搜索 API 配置" }).press("Escape");
  await header.getByRole("button", { name: "设置", exact: true }).click();
  await page.getByRole("dialog", { name: "Codex 设置", exact: true }).waitFor();
  await page.getByRole("button", { name: "关闭弹窗", exact: true }).click();
  assert.equal(
    await header
      .getByRole("button", { name: "设置", exact: true })
      .evaluate((el) => el === document.activeElement),
    true,
  );
  await header.getByRole("button", { name: "添加供应商", exact: true }).click();
  await page.getByRole("dialog", { name: "添加供应商", exact: true }).waitFor();
  await page.getByRole("button", { name: "取消", exact: true }).click();
  checks.push(
    "Codex顶部删除目标说明及重复标题，标题与所有操作共用一行；搜索、设置、添加和返回焦点可用",
  );
  await page.getByRole("tab", { name: /Claude Code/ }).click();
  const desktop = header.getByRole("button", { name: "桌面端", exact: true });
  const cli = header.getByRole("button", { name: "CLI", exact: true });
  await cli.click();
  assert.equal(await cli.getAttribute("aria-pressed"), "true");
  await desktop.click();
  assert.equal(await desktop.getAttribute("aria-pressed"), "true");
  await checkLayout(true);
  await audit("Merged Claude toolbar");
  await page.screenshot({
    path: `docs/screenshots/compact-claude-header-${version}.png`,
  });
  for (const width of [1024, 900, 800, 560, 390]) {
    await page.setViewportSize({ width, height: 780 });
    await checkLayout(width > 560);
  }
  await audit("Merged toolbar 390px");
  await page.screenshot({
    path: `docs/screenshots/compact-header-narrow-${version}.png`,
  });
  checks.push(
    "Claude目标切换嵌入同一工具栏，1120至800px保持一行，窄窗口自然换行且无横向溢出",
  );
  await page.getByRole("tab", { name: /Codex/ }).click();
  await page.setViewportSize({ width: 1120, height: 780 });
}
