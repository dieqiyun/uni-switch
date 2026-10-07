import assert from "node:assert/strict";
import { readFile, writeFile } from "node:fs/promises";
import path from "node:path";

export async function verifyInlineModels({
  page,
  overview,
  invoke,
  snapshot,
  row,
  gpt,
  checks,
  audit,
}) {
  const closeRestart = async () => {
    await page.getByRole("dialog", { name: "重启 Codex 使配置生效" }).waitFor();
    await page.getByRole("button", { name: "稍后重启", exact: true }).click();
  };
  let provider = (await overview()).providers.find((p) => p.id === gpt.id);
  const originalName = provider.name;
  const revisionBeforeName = (await overview()).targets.find(
    (t) => t.target === "codex",
  ).configurationRevision;
  await row(provider)
    .getByRole("button", { name: `修改名称 ${provider.name}`, exact: true })
    .click();
  await row(provider)
    .getByLabel(`供应商名称 · ${provider.name}`, { exact: true })
    .fill("供应商行内名称测试");
  await row(provider)
    .getByRole("button", { name: "保存供应商名称", exact: true })
    .click();
  await row(provider)
    .getByRole("button", { name: "修改名称 供应商行内名称测试", exact: true })
    .waitFor();
  assert.equal(
    (await overview()).targets.find((t) => t.target === "codex")
      .configurationRevision,
    revisionBeforeName,
  );
  assert.equal(await page.getByRole("dialog").count(), 0);
  provider = (await overview()).providers.find((p) => p.id === gpt.id);
  await row(provider)
    .getByRole("button", { name: `修改名称 ${provider.name}`, exact: true })
    .click();
  await row(provider)
    .getByLabel(`供应商名称 · ${provider.name}`, { exact: true })
    .fill(originalName);
  await row(provider)
    .getByRole("button", { name: "保存供应商名称", exact: true })
    .click();
  await row(provider)
    .getByRole("button", { name: `修改名称 ${originalName}`, exact: true })
    .waitFor();
  provider = (await overview()).providers.find((p) => p.id === gpt.id);
  const stale = { ...provider, name: "旧列表记录" };
  const staleBefore = await snapshot();
  await assert.rejects(
    invoke("quick_model_settings", {
      input: {
        expected: stale,
        target: "codex",
        model: provider.model,
        models: provider.codexOptions.models,
      },
    }),
  );
  assert.deepEqual(await snapshot(), staleBefore);
  checks.push(
    "供应商名称在列表直接保存且不修改客户端、不提示重启；过期模型快捷请求原子拒绝",
  );
  const chooseModel = async (entry, model) => {
    await row(entry)
      .getByRole("button", { name: `配置 ${entry.name} 的模型`, exact: true })
      .click();
    const panel = page.getByRole("dialog", {
      name: `${entry.name} · 模型配置`,
      exact: true,
    });
    await page.waitForFunction(
      () => !document.querySelector('.model-config-dialog [aria-busy="true"]'),
    );
    await panel
      .getByRole("button", { name: `将 ${model} 设为默认模型`, exact: true })
      .click();
    await panel
      .getByRole("button", { name: /^(保存并应用|保存模型配置)$/ })
      .click();
    await panel.waitFor({ state: "hidden" });
  };
  const firstModel = provider.model;
  const nextModel = provider.codexOptions.models.find(
    (m) => m.id !== firstModel,
  ).id;
  await chooseModel(provider, nextModel);
  await closeRestart();
  provider = (await overview()).providers.find((p) => p.id === gpt.id);
  assert.equal(provider.model, nextModel);
  assert.equal(
    (await overview()).targets.find((t) => t.target === "codex").appliedModel,
    nextModel,
  );
  assert.equal(await page.getByRole("dialog").count(), 0);
  await chooseModel(provider, firstModel);
  await closeRestart();
  checks.push(
    "模型弹窗确认后写入当前Codex，自动启用并弹重启提示，全程不打开编辑",
  );

  provider = (await overview()).providers.find((p) => p.id === gpt.id);
  const context = row(provider).getByLabel(
    `上下文长度 · ${provider.name} · ${firstModel}`,
    { exact: true },
  );
  const originalContext = await context.inputValue();
  await context.fill("192");
  await context.press("Enter");
  await closeRestart();
  provider = (await overview()).providers.find((p) => p.id === gpt.id);
  assert.equal(
    provider.codexOptions.models.find((m) => m.id === firstModel).contextWindow,
    192000,
  );
  const beforeInvalid = await snapshot();
  await context.fill("wrong");
  await context.press("Enter");
  await row(provider).getByRole("alert").waitFor();
  assert.deepEqual(await snapshot(), beforeInvalid);
  await context.fill("64");
  await context.press("Escape");
  assert.equal(await context.inputValue(), "192");
  assert.deepEqual(await snapshot(), beforeInvalid);
  await context.fill(originalContext);
  await context.press("Enter");
  await closeRestart();
  checks.push(
    "列表逐模型上下文Enter确认，非法值不写入、Escape取消，成功写入后提示重启",
  );

  provider = (await overview()).providers.find((p) => p.id === gpt.id);
  await row(provider)
    .getByRole("button", { name: `配置 ${provider.name} 的模型`, exact: true })
    .click();
  const inline = page.getByRole("dialog", {
    name: `${provider.name} · 模型配置`,
    exact: true,
  });
  await inline
    .getByRole("button", { name: "刷新模型列表", exact: true })
    .click();
  await page.waitForFunction(
    () => !document.querySelector('.model-config-dialog [aria-busy="true"]'),
  );
  await audit("Model dialog selections and contexts");
  await page.screenshot({
    path: "docs/screenshots/model-dialog-0.5.16.png",
  });
  const draftBefore = await snapshot();
  const check = inline.getByLabel(`启用 ${nextModel}`, { exact: true });
  const originalEnabled = await check.isChecked();
  await check.setChecked(!originalEnabled);
  await inline.getByRole("button", { name: "取消", exact: true }).click();
  assert.deepEqual(await snapshot(), draftBefore);
  await row(provider)
    .getByRole("button", { name: `配置 ${provider.name} 的模型`, exact: true })
    .click();
  await inline
    .getByLabel(`启用 ${nextModel}`, { exact: true })
    .setChecked(true);
  await inline.getByRole("button", { name: "保存并应用", exact: true }).click();
  // This may be a no-op selection; no-op saves must not manufacture a restart.
  if (!originalEnabled) await closeRestart();
  await inline.waitFor({ state: "hidden" });
  checks.push(
    "行内模型自动同步、勾选和上下文使用草稿，取消数据库与文件不变，批量确认后写入",
  );

  provider = (await overview()).providers.find((p) => p.id === gpt.id);
  assert.equal(
    await page.getByRole("button", { name: /修复.*思考强度/ }).count(),
    0,
  );
  const codexDirectory = (await overview()).targets.find(
    (t) => t.target === "codex",
  ).directory;
  const catalogPath = path.join(codexDirectory, "uni-switch-models.json");
  const configPath = path.join(codexDirectory, "config.toml");
  const assertRepair = async () => {
    const catalog = JSON.parse(await readFile(catalogPath, "utf8"));
    const config = await readFile(configPath, "utf8");
    const display = config.match(
      /enabled-reasoning-efforts\s*=\s*(\[[^\]]*\])/,
    )[1];
    for (const effort of [
      "none",
      "minimal",
      "low",
      "medium",
      "high",
      "xhigh",
      "max",
      "ultra",
    ]) {
      assert.ok(display.includes(`"${effort}"`));
      for (const model of catalog.models)
        assert.equal(
          model.supported_reasoning_levels.filter((e) => e.effort === effort)
            .length,
          1,
        );
    }
  };
  await assertRepair();
  // Remove only a standard display preset, as can happen in the client. A
  // normal Fast toggle must repair it without a separate user action.
  const catalog = JSON.parse(await readFile(catalogPath, "utf8"));
  for (const model of catalog.models)
    model.supported_reasoning_levels = model.supported_reasoning_levels.filter(
      (e) => e.effort !== "max",
    );
  await writeFile(catalogPath, JSON.stringify(catalog, null, 2) + "\n");
  await writeFile(
    configPath,
    (await readFile(configPath, "utf8")).replace(
      /enabled-reasoning-efforts\s*=\s*\[[^\]]*\]/,
      'enabled-reasoning-efforts = ["high"]',
    ),
  );
  const fast = row(provider).getByRole("switch", {
    name: `Fast 模式 · ${provider.name}`,
    exact: true,
  });
  const wasFast = await fast.isChecked();
  await fast.click();
  await closeRestart();
  await page.waitForFunction(
    ({ name, checked }) => {
      const control = [
        ...document.querySelectorAll('input[role="switch"]'),
      ].find((e) => e.getAttribute("aria-label") === name);
      return control && !control.disabled && control.checked === checked;
    },
    { name: `Fast 模式 · ${provider.name}`, checked: !wasFast },
  );
  await assertRepair();
  await fast.click();
  await closeRestart();
  await page.waitForFunction(
    ({ name, checked }) => {
      const control = [
        ...document.querySelectorAll('input[role="switch"]'),
      ].find((e) => e.getAttribute("aria-label") === name);
      return control && !control.disabled && control.checked === checked;
    },
    { name: `Fast 模式 · ${provider.name}`, checked: wasFast },
  );
  await assertRepair();
  await row(provider)
    .getByRole("button", { name: `刷新 ${provider.name} 余额`, exact: true })
    .click();
  assert.equal(
    await row(provider)
      .getByRole("button", {
        name: `查看 ${provider.name} 余额详情`,
        exact: true,
      })
      .getAttribute("aria-expanded"),
    "false",
  );
  checks.push(
    "界面无手动修复按钮，普通Fast修改自动补回目录和桌面显示中缺失的max；余额行内刷新",
  );
  const successNotice = page.getByRole("button", {
    name: "关闭提示",
    exact: true,
  });
  if (await successNotice.count()) await successNotice.click();
  await page.evaluate(() => document.activeElement?.blur());
  await audit("Inline quick controls");
  await page.screenshot({
    path: "docs/screenshots/inline-provider-list-0.5.16.png",
  });
  await page.getByRole("tab", { name: /Claude Code/ }).click();
  await page.getByRole("button", { name: "CLI", exact: true }).click();
  if (
    (await overview()).targets.find((t) => t.target === "claude_cli").state ===
    "saved_changes"
  ) {
    await row(provider)
      .getByRole("button", { name: "更新配置", exact: true })
      .click();
    await page.waitForFunction(
      () => !document.querySelector('.provider-list[aria-busy="true"]'),
    );
  }
  provider = (await overview()).providers.find((p) => p.id === gpt.id);
  await chooseModel(provider, nextModel);
  await page.waitForFunction(
    () => !document.querySelector('.provider-list[aria-busy="true"]'),
  );
  assert.equal(
    (await overview()).targets.find((t) => t.target === "claude_cli")
      .appliedModel,
    nextModel,
  );
  assert.equal(
    (await overview()).targets.find((t) => t.target === "codex").appliedModel,
    firstModel,
  );
  provider = (await overview()).providers.find((p) => p.id === gpt.id);
  await chooseModel(provider, firstModel);
  await page.waitForFunction(
    () => !document.querySelector('.provider-list[aria-busy="true"]'),
  );
  await page.getByRole("button", { name: "桌面端", exact: true }).click();
  if (
    (await overview()).targets.find((t) => t.target === "claude_desktop")
      .state === "saved_changes"
  ) {
    await row(provider)
      .getByRole("button", { name: "更新配置", exact: true })
      .click();
    await page.waitForFunction(
      () => !document.querySelector('.provider-list[aria-busy="true"]'),
    );
  }
  await chooseModel(provider, nextModel);
  await page.waitForFunction(
    () => !document.querySelector('.provider-list[aria-busy="true"]'),
  );
  assert.equal(
    (await overview()).targets.find((t) => t.target === "claude_desktop")
      .appliedModel,
    nextModel,
  );
  provider = (await overview()).providers.find((p) => p.id === gpt.id);
  await chooseModel(provider, firstModel);
  await page.waitForFunction(
    () => !document.querySelector('.provider-list[aria-busy="true"]'),
  );
  await page.getByRole("tab", { name: /Codex/ }).click();
  checks.push(
    "Claude桌面和CLI也能在列表选择GPT模型，分别写入各自目标，Codex已应用模型保持不变",
  );
}
