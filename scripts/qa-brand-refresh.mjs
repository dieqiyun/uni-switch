import assert from "node:assert/strict";
import path from "node:path";
import { mkdir, readFile, rmdir, unlink } from "node:fs/promises";

export async function verifyCenteredPromotion(page) {
  const footer = page.locator(".workspace-footer");
  const promotion = footer.locator(".service-promotion");
  const status = footer.locator(".footer-status");
  const bounds = await footer.boundingBox();
  const content = await promotion.boundingBox();
  const state = await status.boundingBox();
  assert.ok(Math.abs(content.x + content.width / 2 - bounds.x - bounds.width / 2) <= 1, "Promotion is centered in the workspace footer");
  assert.ok(content.x >= bounds.x && content.x + content.width <= bounds.x + bounds.width, "Promotion stays inside the footer");
  const overlapX = Math.min(state.x + state.width, content.x + content.width) - Math.max(state.x, content.x);
  const overlapY = Math.min(state.y + state.height, content.y + content.height) - Math.max(state.y, content.y);
  assert.ok(overlapX <= 1 || overlapY <= 1, "Local/bridge status does not overlap promotion");
  for (const element of [footer, status, status.locator(".footer-status-text"), promotion]) {
    assert.equal(await element.evaluate((el) => el.scrollWidth > el.clientWidth + 1), false);
  }
}

export async function verifyBrandRefresh({
  page,
  snapshot,
  root,
  checks,
  audit,
}) {
  const before = await snapshot();
  const footer = page.locator(".workspace-footer");
  const link = footer.getByRole("link", {
    name: "访问蝶祈云 API 官网（在浏览器中打开）",
    exact: true,
  });
  const marker = path.join(root, "opened-service.txt");
  assert.equal(await link.getAttribute("href"), "https://www.dieqiyun.top/");
  assert.equal(await link.getAttribute("target"), "_blank");
  assert.equal(await page.locator(".brand-mark").count(), 0);
  assert.equal(
    await page
      .locator(".brand-logo")
      .evaluate((el) => el.complete && el.naturalWidth === 256),
    true,
  );
  assert.equal(
    await footer
      .locator(".service-promotion-logo")
      .evaluate((el) => el.complete && el.naturalWidth === 128),
    true,
  );
  // Inspect the actual loaded asset and the rendered core controls. This catches
  // stale colored artwork or a competing stylesheet after the palette change.
  const colors = await page.evaluate(() => {
    const logo = document.querySelector(".brand-logo");
    const canvas = document.createElement("canvas");
    canvas.width = logo.naturalWidth;
    canvas.height = logo.naturalHeight;
    const ctx = canvas.getContext("2d");
    ctx.drawImage(logo, 0, 0);
    const pixels = ctx.getImageData(0, 0, canvas.width, canvas.height).data;
    let colored = 0;
    let visible = 0;
    for (let i = 0; i < pixels.length; i += 4) {
      if (pixels[i + 3] < 10) continue;
      visible++;
      if (Math.max(...pixels.slice(i, i + 3)) - Math.min(...pixels.slice(i, i + 3)) > 2) colored++;
    }
    const neutral = (value) => {
      const channels = value.match(/[\d.]+/g).slice(0, 3).map(Number);
      return Math.max(...channels) - Math.min(...channels) <= 2;
    };
    const selectors = [".sidebar", ".brand", ".workspace-footer", ".service-promotion-link", ".header-tools .primary", ".applied-button", ".provider-fast-track"];
    return {
      colored, visible,
      controls: selectors.map((selector) => {
        const style = getComputedStyle(document.querySelector(selector));
        return { selector, neutral: neutral(style.color) && neutral(style.backgroundColor) };
      }),
    };
  });
  assert.ok(colors.visible > 100, "Logo contains visible artwork");
  assert.equal(colors.colored, 0, "Loaded Logo is monochrome");
  assert.ok(colors.controls.every(({ neutral }) => neutral), JSON.stringify(colors.controls));
  for (const logo of [
    footer.locator(".service-promotion-logo"),
    page.getByRole("tab", { name: /Claude Code/ }).locator(".app-glyph img"),
  ]) {
    const result = await logo.evaluate((el) => {
      const canvas = document.createElement("canvas");
      canvas.width = el.naturalWidth;
      canvas.height = el.naturalHeight;
      const ctx = canvas.getContext("2d");
      ctx.drawImage(el, 0, 0);
      const pixels = ctx.getImageData(0, 0, canvas.width, canvas.height).data;
      let colored = 0;
      for (let i = 0; i < pixels.length; i += 4) {
        if (pixels[i + 3] > 100 && Math.max(...pixels.slice(i, i + 3)) - Math.min(...pixels.slice(i, i + 3)) > 20) colored++;
      }
      return { colored, filter: getComputedStyle(el).filter };
    });
    assert.ok(result.colored > 100, "Service and Claude logos keep colored pixels");
    assert.equal(result.filter, "none", "Brand logo is rendered in its original colors");
  }
  await verifyCenteredPromotion(page);
  await link.focus();
  await page.keyboard.press("Enter");
  await page.waitForFunction(
    () =>
      !document
        .querySelector(".service-promotion-link")
        .hasAttribute("aria-busy"),
  );
  assert.equal(await readFile(marker, "utf8"), "https://www.dieqiyun.top/");
  assert.equal(
    await link.evaluate((el) => el === document.activeElement),
    true,
  );
  assert.deepEqual(await snapshot(), before);
  checks.push(
    "uni-switch保持黑白标志；Claude官方彩色Logo与蝶祈云紫色Logo实际加载且未滤成灰色；宣传居中，官网键盘打开固定地址，文件不变",
  );
  await audit("Brand and service link");
  await link.evaluate((el) => el.blur());
  await page.screenshot({ path: "docs/screenshots/centered-service-local.png" });
  await footer.screenshot({
    path: "docs/screenshots/centered-service-footer-local.png",
  });

  // A directory at the exact isolated marker path makes the launch fail without
  // opening a real browser. Remove only that empty directory before retrying.
  await unlink(marker);
  await mkdir(marker);
  await link.click();
  await footer.getByRole("alert").waitFor();
  assert.ok(
    (await footer.getByRole("alert").textContent()).includes(
      "https://www.dieqiyun.top/",
    ),
  );
  await audit("Service launch error");
  await rmdir(marker);
  await link.click();
  await footer.getByRole("alert").waitFor({ state: "hidden" });
  assert.equal(await readFile(marker, "utf8"), "https://www.dieqiyun.top/");
  for (const size of [
    { width: 1120, height: 780 },
    { width: 1001, height: 780 },
    { width: 1000, height: 780 },
    { width: 920, height: 720 },
    { width: 760, height: 600 },
    { width: 390, height: 620 },
  ]) {
    await page.setViewportSize(size);
    await verifyCenteredPromotion(page);
    assert.equal(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
      false,
    );
    const bounds = await footer.boundingBox();
    const action = await link
      .locator(".service-promotion-action")
      .boundingBox();
    assert.ok(bounds.y >= 0 && bounds.y + bounds.height <= size.height + 1);
    assert.ok(action.x >= 0 && action.x + action.width <= size.width);
    assert.ok(
      action.y >= bounds.y &&
        action.y + action.height <= bounds.y + bounds.height,
    );
    assert.equal(
      await footer.evaluate((el) => el.scrollWidth > el.clientWidth),
      false,
    );
    await audit(`Brand footer ${size.width}`);
  }
  await page.screenshot({
    path: "docs/screenshots/centered-service-narrow-local.png",
  });
  await page.setViewportSize({ width: 1120, height: 780 });
  assert.deepEqual(await snapshot(), before);
  checks.push(
    "官网失败可重试；1120/1001/1000/920/760/390px宣传保持水平居中，状态不重叠、官网入口可见，无溢出或Axe违规",
  );
}
