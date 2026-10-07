import { chromium } from "playwright";
import AxeBuilder from "@axe-core/playwright";
import { spawn } from "node:child_process";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import assert from "node:assert/strict";

const root = path.resolve(".qa/directory-discovery/native", String(Date.now()));
const home = path.join(root, "用户 中文");
const local = path.join(home, "Local");
const defaultCodex = path.join(home, ".codex");
const active = path.join(home, "tools", "Codex 实际配置");
const backup = path.join(home, "backup", ".codex");
const project = path.join(home, "projects", "demo");
const other = path.join(home, "Codex 第二份");
const cli = path.join(home, ".claude");
const contextFile = path.join(root, "context.json");
const context = {
  home,
  local,
  roaming: path.join(home, "Roaming"),
  roots: [home],
  hints: [],
  notes: [],
};
const put = async (file, text) => {
  await mkdir(path.dirname(file), { recursive: true });
  await writeFile(file, text, "utf8");
};
const updateContext = () =>
  writeFile(contextFile, JSON.stringify(context), "utf8");
await mkdir(local, { recursive: true });
await mkdir("docs/screenshots", { recursive: true });
await put(
  path.join(active, "config.toml"),
  "# keep this config\nmodel='old'\n",
);
await put(path.join(backup, "config.toml"), "model='backup'\n");
await put(path.join(project, "package.json"), "{}");
await put(path.join(project, ".codex/config.toml"), "model='project'\n");
await updateContext();
const checks = [],
  accessibility = [],
  pageErrors = [];
const same = (a, b) =>
  path.resolve(a).toLowerCase() === path.resolve(b).toLowerCase();
const waitUntil = async (fn, ms = 15000) => {
  const until = Date.now() + ms;
  while (Date.now() < until) {
    if (await fn()) return;
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error("Native directory QA timed out");
};
const portOpen = async () => {
  try {
    return (await fetch("http://127.0.0.1:9223/json/version")).ok;
  } catch {
    return false;
  }
};
let child, browser, page;
const invoke = async (name, args = {}) => {
  const response = await page.evaluate(
    async ({ name, args }) => {
      try {
        return { value: await window.__TAURI_INTERNALS__.invoke(name, args) };
      } catch (error) {
        return { error };
      }
    },
    { name, args },
  );
  if (response.error)
    throw Object.assign(
      new Error(response.error.message || String(response.error)),
      response.error,
    );
  return response.value;
};
const status = async (target = "codex") =>
  (await invoke("get_overview")).targets.find((s) => s.target === target);
const launch = async () => {
  assert.equal(
    await portOpen(),
    false,
    "Do not connect to another task's QA window",
  );
  child = spawn(
    path.resolve(
      process.env.UNI_SWITCH_QA_EXE ||
        ".qa/directory-discovery/test-app/uni-switch.exe",
    ),
    [],
    {
      windowsHide: true,
      stdio: "ignore",
      env: {
        ...process.env,
        UNI_SWITCH_QA_DISCOVERY_CONTEXT: contextFile,
        UNI_SWITCH_DATA_DIR: path.join(root, "data"),
        WEBVIEW2_USER_DATA_FOLDER: path.join(root, "webview"),
        CODEX_HOME: defaultCodex,
        CLAUDE_CONFIG_DIR: cli,
        LOCALAPPDATA: local,
      },
    },
  );
  await waitUntil(portOpen, 20000);
  browser = await chromium.connectOverCDP("http://127.0.0.1:9223");
  page = browser.contexts()[0].pages()[0];
  page.setDefaultTimeout(15000);
  page.on("pageerror", (error) => pageErrors.push(error.message));
  await page.getByRole("button", { name: "设置", exact: true }).waitFor();
};
const stop = async () => {
  if (browser) {
    await browser.close();
    browser = null;
  }
  if (child && child.exitCode === null)
    await new Promise((resolve) => {
      child.once("exit", resolve);
      child.kill();
    });
  child = null;
  await waitUntil(async () => !(await portOpen()));
};
const openSettings = async () => {
  await page.getByRole("button", { name: "设置", exact: true }).click();
  await page.getByRole("button", { name: "自动查找", exact: true }).waitFor();
  await waitUntil(() =>
    page.getByRole("button", { name: "自动查找", exact: true }).isEnabled(),
  );
};
const refresh = async () => {
  await page.getByRole("button", { name: "自动查找", exact: true }).click();
  await waitUntil(() =>
    page.getByRole("button", { name: "自动查找", exact: true }).isEnabled(),
  );
};
const closeSettings = () =>
  page.getByRole("button", { name: "关闭弹窗", exact: true }).click();
const audit = async (label) => {
  await page.evaluate(() =>
    Promise.all(
      document
        .getAnimations()
        .filter((a) => Number.isFinite(a.effect?.getTiming().iterations))
        .map((a) => a.finished.catch(() => {})),
    ),
  );
  const result = await new AxeBuilder({ page }).analyze();
  accessibility.push({
    label,
    violations: result.violations.map((v) => ({
      id: v.id,
      targets: v.nodes.map((n) => n.target),
    })),
  });
  await writeFile(
    path.join(root, "accessibility.json"),
    JSON.stringify(accessibility, null, 2),
  );
  assert.deepEqual(
    result.violations.map((v) => v.id),
    [],
    label,
  );
};
try {
  await launch();
  assert.ok(same((await status()).directory, defaultCodex));
  await openSettings();
  assert.ok(same((await status()).directory, active));
  await page.getByText(/已自动选择找到的配置位置/).waitFor();
  assert.equal(
    await page
      .locator(".directory-candidates > li")
      .first()
      .getAttribute("class"),
    "directory-candidate is-current",
  );
  assert.equal(
    await readFile(path.join(active, "config.toml"), "utf8"),
    "# keep this config\nmodel='old'\n",
  );
  await page.getByText("备份副本", { exact: true }).waitFor();
  await page.getByText("项目局部配置", { exact: true }).waitFor();
  const discovered = await invoke("discover_config_directories", {
    target: "codex",
  });
  assert.equal(discovered.canAutoSelect, true);
  assert.equal(
    discovered.candidates.filter(
      (c) => c.kind === "global" && c.files.length && c.usable,
    ).length,
    1,
  );
  checks.push(
    "打开设置自动搜索；唯一全局配置自动绑定；搜索不改写文件；排除备份和项目局部配置",
  );
  await audit("Unique automatic directory");
  await page.screenshot({ path: "docs/screenshots/directory-discovery.png" });

  await put(path.join(other, "config.toml"), "model='other'\n");
  await refresh();
  assert.ok(same((await status()).directory, active));
  await page.getByText(/多个可读取的全局配置/).waitFor();
  const pick = page.getByRole("button", {
    name: `使用目录 ${other}`,
    exact: true,
  });
  await pick.focus();
  await page.keyboard.press("Enter");
  await page.getByText(/配置位置已更新/).waitFor();
  assert.ok(same((await status()).directory, other));
  await page.locator(".directory-candidate.is-current summary").click();
  await audit("Multiple candidates and keyboard selection");
  await page.screenshot({
    path: "docs/screenshots/directory-discovery-multiple.png",
  });
  await page.setViewportSize({ width: 390, height: 844 });
  assert.equal(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    ),
    false,
  );
  await audit("Narrow settings dialog");
  await page.screenshot({
    path: "docs/screenshots/directory-discovery-narrow.png",
  });
  await page.setViewportSize({ width: 1120, height: 780 });
  await closeSettings();
  await stop();
  await launch();
  assert.ok(same((await status()).directory, other));
  context.hints = [
    {
      target: "codex",
      directory: active,
      evidence: "运行中的 Codex（隔离进程线索）",
      running: true,
    },
  ];
  await updateContext();
  await openSettings();
  assert.ok(same((await status()).directory, other));
  await page.getByText(/运行中的客户端使用另一目录/).waitFor();
  checks.push(
    "多个有效目录不按修改时间自动切换；键盘一键选择；重启记住手动选择；与运行进程冲突时保留选择并提示",
  );

  const provider = await invoke("save_provider", {
    input: {
      family: "codex",
      name: "目录 QA",
      baseUrl: "https://example.test/v1",
      apiKey: "fake-directory-test-key",
      model: "gpt-directory-test",
      authMode: "bearer",
      codexOptions: {
        models: [
          {
            id: "gpt-directory-test",
            contextWindow: 256000,
            enabled: true,
            reasoningEfforts: [],
          },
        ],
      },
    },
  });
  await invoke("apply_provider", { target: "codex", providerId: provider.id });
  await closeSettings();
  await page.reload();
  await openSettings();
  const otherButtons = page.getByRole("button", { name: /^使用目录/ });
  for (const button of await otherButtons.all())
    assert.equal(await button.isDisabled(), true);
  await assert.rejects(
    invoke("select_detected_directory", {
      target: "codex",
      directory: active,
      expectedDirectory: other,
      automatic: false,
    }),
    (error) => error.code === "managed_directory",
  );
  await invoke("restore_original", { target: "codex" });
  assert.match(
    await readFile(path.join(other, "config.toml"), "utf8"),
    /^model\s*=\s*'other'\s*$/,
  );
  checks.push(
    "已接管目录不自动更换；UI 禁用更换并说明原因；后端拒绝绕过；恢复原配置保留原文件",
  );
  await closeSettings();

  const outside = path.join(root, "外部搜索");
  const external = path.join(outside, "nested", "custom-home");
  await put(path.join(external, "config.toml"), "model='external'\n");
  await put(path.join(external, "auth.json"), "{}");
  const expanded = await invoke("discover_config_directories", {
    target: "codex",
    searchRoot: outside,
  });
  assert.ok(expanded.candidates.some((c) => same(c.directory, external)));
  assert.ok(same((await status()).directory, other));
  await assert.rejects(
    invoke("select_detected_directory", {
      target: "codex",
      directory: active,
      expectedDirectory: defaultCodex,
      automatic: false,
    }),
    (error) => error.code === "directory_changed",
  );
  checks.push(
    "额外文件夹搜索找到任意名称的 Codex Home；搜索不切换绑定；过期搜索不能覆盖后来选择",
  );

  const mainConfig = path.join(local, "Claude/claude_desktop_config.json");
  await put(mainConfig, '{"deploymentMode":"consumer","unrelated":true}');
  await put(
    path.join(local, "Claude-3p/configLibrary/_meta.json"),
    '{"appliedId":"existing","entries":[{"id":"existing"}]}',
  );
  await put(
    path.join(local, "Claude-3p/configLibrary/existing.json"),
    '{"inferenceApiKey":"do-not-output-this-key"}',
  );
  const variant = path.join(local, "Claude-Beta-3p");
  await put(path.join(variant, "claude_desktop_config.json"), "{}");
  await page.getByRole("tab", { name: /Claude Code/ }).click();
  await openSettings();
  const desktop = await invoke("discover_config_directories", {
    target: "claude_desktop",
  });
  assert.equal(desktop.canAutoSelect, false);
  assert.equal(
    desktop.candidates.filter((c) => c.usable && c.files.length).length,
    2,
  );
  assert.ok(!JSON.stringify(desktop).includes("do-not-output-this-key"));
  await page
    .getByRole("button", { name: `使用目录 ${variant}`, exact: true })
    .click();
  await page.getByText(/配置位置已更新/).waitFor();
  const claude = await invoke("save_provider", {
    input: {
      family: "claude",
      name: "Claude 目录 QA",
      baseUrl: "https://example.test",
      apiKey: "fake-claude-directory-key",
      model: "claude-sonnet-4-6",
      authMode: "x-api-key",
    },
  });
  await invoke("apply_provider", {
    target: "claude_desktop",
    providerId: claude.id,
  });
  const profile = JSON.parse(
    await readFile(
      path.join(
        variant,
        "configLibrary/e82de475-47fa-4c54-9000-13571c000001.json",
      ),
      "utf8",
    ),
  );
  assert.equal(profile.inferenceGatewayApiKey, "fake-claude-directory-key");
  assert.equal(
    await readFile(mainConfig, "utf8"),
    '{"deploymentMode":"consumer","unrelated":true}',
  );
  await invoke("restore_original", { target: "claude_desktop" });
  await closeSettings();
  checks.push(
    "Claude 桌面标准目录与其他安装实例分别显示；检查当前 profile；不暴露 Key；只写入选中实例且可恢复",
  );

  const customCli = path.join(home, "cli-custom");
  await put(
    path.join(customCli, "settings.json"),
    '{"permissions":{"defaultMode":"default"}}',
  );
  context.hints = [
    {
      target: "claude_cli",
      directory: customCli,
      evidence: "CLAUDE_CONFIG_DIR 环境变量（未确认）",
      running: false,
    },
  ];
  await updateContext();
  const cliHint = await invoke("discover_config_directories", {
    target: "claude_cli",
  });
  assert.equal(cliHint.canAutoSelect, false);
  assert.ok(same(cliHint.recommendedDirectory, customCli));
  context.hints[0].running = true;
  context.hints[0].evidence = "运行中的 Claude CLI（隔离进程线索）";
  await updateContext();
  const cliRunning = await invoke("discover_config_directories", {
    target: "claude_cli",
  });
  assert.equal(cliRunning.canAutoSelect, true);
  await invoke("select_detected_directory", {
    target: "claude_cli",
    directory: customCli,
    expectedDirectory: cli,
    automatic: true,
  });
  assert.ok(same((await status("claude_cli")).directory, customCli));
  checks.push(
    "Claude CLI 环境线索只推荐；进程确认后允许自动选择；自动绑定由后端重新核实",
  );
  assert.deepEqual(pageErrors, []);
  const report = {
    version: "0.3.9",
    passed: checks.length,
    checks,
    accessibility,
    pageErrors,
    runDirectory: root,
    fixtureProcessHints: true,
  };
  await writeFile(
    path.join(root, "results.json"),
    JSON.stringify(report, null, 2),
  );
  await writeFile(
    path.resolve(".qa/directory-discovery/results.json"),
    JSON.stringify(report, null, 2),
  );
  console.log(JSON.stringify(report, null, 2));
} catch (error) {
  if (page) {
    console.error(
      await page
        .locator("body")
        .innerText()
        .catch(() => "Window closed"),
    );
    await page
      .screenshot({ path: path.join(root, "failure.png") })
      .catch(() => {});
  }
  throw error;
} finally {
  await stop();
}
