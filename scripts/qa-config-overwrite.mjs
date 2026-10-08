import assert from "node:assert/strict";
import http from "node:http";
import path from "node:path";
import { spawn } from "node:child_process";
import {
  copyFile,
  mkdir,
  readFile,
  readdir,
  writeFile,
} from "node:fs/promises";
import { chromium } from "playwright";
import AxeBuilder from "@axe-core/playwright";

const root = path.resolve(".qa/config-overwrite", String(Date.now()));
const home = path.join(root, "隔离用户 中文");
const codex = path.join(home, ".codex");
const cli = path.join(home, ".claude");
const desktop = path.join(root, "Local");
const data = path.join(root, "data");
await Promise.all(
  [codex, cli, desktop].map((p) => mkdir(p, { recursive: true })),
);
await writeFile(
  path.join(codex, "config.toml"),
  "# isolated original\nmodel='original'\nsandbox_mode='workspace-write'\n",
);
await writeFile(
  path.join(cli, "settings.json"),
  JSON.stringify({
    env: { CUSTOM_QA: "preserved", ANTHROPIC_MODEL: "original" },
  }),
);
const contextFile = path.join(root, "context.json");
await writeFile(
  contextFile,
  JSON.stringify({
    home,
    local: desktop,
    roaming: null,
    roots: [home],
    notes: [],
    hints: [],
  }),
);
const key = "synthetic-overwrite-qa-key";
const checks = [],
  audits = [],
  errors = [];
const server = http.createServer((req, res) => {
  const send = (body, code = 200) => {
    res.writeHead(code, { "Content-Type": "application/json" });
    res.end(JSON.stringify(body));
  };
  if (req.url === "/github/releases/latest") return send({}, 404);
  if (req.url.endsWith("/models")) {
    const anthropic = req.url.startsWith("/claude");
    if (
      anthropic
        ? req.headers["x-api-key"] !== key
        : req.headers.authorization !== "Bearer " + key
    )
      return send({}, 401);
    return send(
      anthropic
        ? {
            data: [{ id: "claude-sonnet-4-6", type: "model" }],
            has_more: false,
          }
        : { object: "list", data: [{ id: "gpt-5.4", object: "model" }] },
    );
  }
  if (req.url.endsWith("/usage"))
    return send({ balance: 7.83, unit: "USD", mode: "unrestricted" });
  send({}, 404);
});
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const origin = "http://127.0.0.1:" + server.address().port;
let app, browser, page;
async function until(fn) {
  const end = Date.now() + 20000;
  while (Date.now() < end) {
    if (await fn()) return;
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error("Overwrite QA timed out");
}
async function portOpen() {
  try {
    return (await fetch("http://127.0.0.1:9223/json/version")).ok;
  } catch {
    return false;
  }
}
async function invoke(name, args = {}) {
  const result = await page.evaluate(
    async ({ name, args }) => {
      try {
        return { value: await window.__TAURI_INTERNALS__.invoke(name, args) };
      } catch (error) {
        return { error };
      }
    },
    { name, args },
  );
  if (result.error)
    throw Object.assign(new Error(result.error.message), result.error);
  return result.value;
}
const overview = () => invoke("get_overview");
const status = async (target) =>
  (await overview()).targets.find((value) => value.target === target);
const row = (provider) =>
  page.locator(".provider-card").filter({
    has: page.getByRole("button", {
      name: "修改名称 " + provider.name,
      exact: true,
      includeHidden: true,
    }),
  });
const dialog = () =>
  page.getByRole("dialog", { name: "覆盖现有 API 配置？", exact: true });
async function files(target) {
  return Promise.all(
    (await status(target)).files.map(async (file) => [
      file,
      await readFile(file, "utf8").catch((error) => {
        if (error.code === "ENOENT") return null;
        throw error;
      }),
    ]),
  );
}
async function allFiles() {
  return Object.fromEntries(
    await Promise.all(
      ["codex", "claude_desktop", "claude_cli"].map(async (target) => [
        target,
        await files(target),
      ]),
    ),
  );
}
async function refresh() {
  const button = page.getByRole("button", {
    name: "刷新配置状态",
    exact: true,
  });
  await until(() => button.isEnabled());
  await button.click();
  await until(() => button.isEnabled());
}
async function choose(target) {
  if (target === "codex")
    await page
      .getByRole("tab", { name: "Codex 桌面端与 CLI", exact: true })
      .click();
  else {
    await page
      .getByRole("tab", { name: "Claude Code 桌面端与 CLI", exact: true })
      .click();
    await page
      .getByRole("button", {
        name: target === "claude_cli" ? "CLI" : "桌面端",
        exact: true,
      })
      .click();
  }
}
async function dismissRestart(target) {
  if (target !== "codex") {
    const runtime = await invoke("get_runtime_status", { target });
    const pending =
      target === "claude_cli"
        ? runtime.clientRunning && runtime.restartRequired
        : runtime.desktopRunning && runtime.desktopRestartRequired;
    if (!pending) {
      assert.equal(
        runtime.clientRunning,
        false,
        "Isolated Claude target has no running client",
      );
      await until(() =>
        page
          .getByRole("button", { name: "刷新配置状态", exact: true })
          .isEnabled(),
      );
      return;
    }
  }
  const title = {
    codex: "重启 Codex 使配置生效",
    claude_desktop: "重启 Claude Code 桌面端使配置生效",
    claude_cli: "重启 Claude CLI 使配置生效",
  }[target];
  const restart = page.getByRole("dialog", { name: title, exact: true });
  await restart.waitFor();
  await restart.getByRole("button", { name: "稍后重启", exact: true }).click();
  await restart.waitFor({ state: "hidden" });
}
async function corrupt(target, label) {
  const entries = await files(target);
  let file, contents;
  if (target === "codex") {
    [file, contents] = entries.find(([p]) => p.endsWith("config.toml"));
    contents = contents.replace(
      /^\s*base_url\s*=.*$/m,
      'base_url = "https://external.invalid/' + label + '"',
    );
    contents += "\n[qa_overwrite_" + label + ']\nnote = "' + label + '"\n';
  } else if (target === "claude_cli") {
    [file, contents] = entries.find(([p]) => p.endsWith("settings.json"));
    const doc = JSON.parse(contents);
    doc.env.ANTHROPIC_BASE_URL = "https://external.invalid/" + label;
    doc.qaOverwrite = label;
    contents = JSON.stringify(doc);
  } else {
    [file, contents] = entries.find(
      ([p]) => p.includes("configLibrary") && !p.endsWith("_meta.json"),
    );
    const doc = JSON.parse(contents);
    doc.inferenceGatewayBaseUrl = "https://external.invalid/" + label;
    doc.qaOverwrite = label;
    contents = JSON.stringify(doc);
  }
  await writeFile(file, contents);
  return { file, contents };
}
async function audit(label) {
  const result = await new AxeBuilder({ page }).analyze();
  const violations = result.violations.map((value) => ({
    id: value.id,
    targets: value.nodes.map((n) => n.target),
  }));
  audits.push({ label, violations });
  assert.deepEqual(violations, []);
}
try {
  assert.equal(await portOpen(), false, "QA CDP port must be free");
  const executable = path.join(root, "uni-switch.exe");
  await copyFile(
    path.resolve("src-tauri/target/debug/uni-switch.exe"),
    executable,
  );
  app = spawn(executable, [], {
    windowsHide: true,
    stdio: "ignore",
    env: {
      ...process.env,
      HOME: home,
      UNI_SWITCH_DATA_DIR: data,
      UNI_SWITCH_QA_DISCOVERY_CONTEXT: contextFile,
      WEBVIEW2_USER_DATA_FOLDER: path.join(root, "webview"),
      CODEX_HOME: codex,
      CLAUDE_CONFIG_DIR: cli,
      LOCALAPPDATA: desktop,
      UNI_SWITCH_QA_UPDATE_REPOSITORY: "example/uni-switch",
      UNI_SWITCH_QA_UPDATE_ENDPOINT: origin + "/github/releases/latest",
    },
  });
  await until(portOpen);
  browser = await chromium.connectOverCDP("http://127.0.0.1:9223");
  page = browser.contexts()[0].pages()[0];
  page.setDefaultTimeout(15000);
  page.on("pageerror", (error) => errors.push(error.message));
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.setViewportSize({ width: 1120, height: 780 });
  await page.getByRole("button", { name: "设置", exact: true }).waitFor();
  for (const [target, directory] of [
    ["codex", codex],
    ["claude_cli", cli],
    ["claude_desktop", desktop],
  ])
    await invoke("set_directory", { target, directory });
  async function add(family, suffix) {
    const model = family === "codex" ? "gpt-5.4" : "claude-sonnet-4-6";
    return invoke("save_provider", {
      input: {
        id: null,
        name: (family === "codex" ? "OpenAI " : "Claude ") + suffix,
        family,
        baseUrl: origin + "/" + family + "-" + suffix + "/v1",
        apiKey: key,
        authMode: family === "codex" ? "bearer" : "x-api-key",
        model,
        reasoningEffort: null,
        codexOptions: {
          upstreamProtocol: family === "codex" ? "openai" : "anthropic",
          protocolDetectedAt: Date.now(),
          models: [
            {
              id: model,
              contextWindow: 256000,
              reasoningEfforts: [],
              enabled: true,
            },
          ],
          fastMode: false,
        },
      },
    });
  }
  const openaiA = await add("codex", "A"),
    openaiB = await add("codex", "B");
  const claudeA = await add("claude", "A"),
    claudeB = await add("claude", "B");
  await refresh();
  const scenarios = [
    ["codex", openaiA, openaiB],
    ["claude_desktop", claudeA, claudeB],
    ["claude_cli", claudeA, claudeB],
  ];
  for (const [target, a, b] of scenarios) {
    await choose(target);
    await row(a).getByRole("button", { name: "使用", exact: true }).click();
    await dismissRestart(target);
    const external = await corrupt(target, target);
    const before = await allFiles();
    const revision = (await status(target)).configurationRevision;
    await refresh();
    await row(a).getByText("配置需处理", { exact: true }).waitFor();
    assert.equal(
      await row(b)
        .getByRole("button", { name: "使用", exact: true })
        .isEnabled(),
      true,
    );
    await row(b).getByRole("button", { name: "使用", exact: true }).click();
    await dialog().waitFor();
    assert.ok((await dialog().textContent()).includes(b.name));
    assert.ok(
      (await dialog().textContent()).includes((await status(target)).directory),
    );
    assert.equal(
      await dialog()
        .getByRole("button", { name: "取消", exact: true })
        .evaluate((el) => el === document.activeElement),
      true,
    );
    assert.deepEqual(
      await allFiles(),
      before,
      "Preparation must not change any client",
    );
    await audit(target + " overwrite confirmation");
    await dialog().getByRole("button", { name: "取消", exact: true }).click();
    await dialog().waitFor({ state: "hidden" });
    assert.deepEqual(await allFiles(), before, "Cancel must not write");
    assert.equal(
      await row(b)
        .getByRole("button", { name: "使用", exact: true })
        .evaluate((el) => el === document.activeElement),
      true,
    );
    await row(b).getByRole("button", { name: "使用", exact: true }).click();
    await dialog().waitFor();
    await page
      .locator(".modal-overlay")
      .click({ position: { x: 3, y: 3 }, force: true });
    assert.equal(
      await dialog().isVisible(),
      true,
      "Outside click must not accept or close",
    );
    await page.keyboard.press("Escape");
    await dialog().waitFor({ state: "hidden" });
    assert.deepEqual(await allFiles(), before, "Escape must not write");
    await row(b).getByRole("button", { name: "使用", exact: true }).click();
    await dialog().waitFor();
    if (target === "codex") {
      for (const size of [
        { width: 1120, height: 780 },
        { width: 560, height: 520 },
        { width: 390, height: 620 },
      ]) {
        await page.setViewportSize(size);
        await dialog()
          .getByText(/将更新 \d+ 个配置文件/)
          .click();
        assert.equal(
          await dialog().evaluate((el) => el.scrollWidth > el.clientWidth),
          false,
        );
        const confirm = dialog().getByRole("button", {
          name: "强制覆盖并使用",
          exact: true,
        });
        await confirm.scrollIntoViewIfNeeded();
        const rect = await confirm.boundingBox();
        assert.ok(
          rect.y >= 0 && rect.y + rect.height <= size.height,
          "Confirmation button remains reachable",
        );
        await audit("Overwrite dialog " + size.width + "x" + size.height);
        await dialog()
          .getByText(/将更新 \d+ 个配置文件/)
          .click();
      }
      await page.setViewportSize({ width: 1120, height: 780 });
      await page.screenshot({ path: path.join(root, "confirmation.png") });
      checks.push(
        "覆盖确认弹窗在1120/560/390px窗口可操作，文件路径可读且没有横向溢出",
      );
    }
    await dialog()
      .getByRole("button", { name: "强制覆盖并使用", exact: true })
      .click();
    await dismissRestart(target);
    const after = await allFiles();
    assert.equal((await status(target)).activeProviderId, b.id);
    assert.equal((await status(target)).state, "applied");
    assert.ok((await status(target)).configurationRevision > revision);
    for (const other of Object.keys(before).filter((v) => v !== target))
      assert.deepEqual(after[other], before[other]);
    const changed = await readFile(external.file, "utf8");
    assert.ok(
      changed.includes(b.baseUrl),
      "Confirmed provider address is written",
    );
    assert.ok(changed.includes(target), "Unrelated setting is preserved");
    assert.ok(
      !changed.includes("https://external.invalid/"),
      "External API address is replaced",
    );
    const backupNames = await readdir(path.join(data, "backups"));
    const backups = await Promise.all(
      backupNames.map(async (file) =>
        JSON.parse(await readFile(path.join(data, "backups", file), "utf8")),
      ),
    );
    assert.ok(
      backups.some(
        (backup) =>
          backup.target === target &&
          backup.changes.some(
            (change) =>
              path.resolve(change.path) === path.resolve(external.file) &&
              change.before === external.contents,
          ),
      ),
      "Backup contains exact pre-overwrite external file",
    );
    checks.push(
      target +
        "：冲突仍可点击使用；取消、Escape、遮罩不写文件；确认后备份完整外部文件、保留无关项、切换供应商，其他客户端不变；Codex提示重启，未运行的Claude下次启动读取配置",
    );
  }
  await choose("codex");
  const external = await corrupt("codex", "stale");
  await refresh();
  await row(openaiA).getByRole("button", { name: "使用", exact: true }).click();
  await dialog().waitFor();
  const contents = external.contents.replace(
    'note = "stale"',
    'note = "changed-during-confirmation"',
  );
  await writeFile(external.file, contents);
  const beforeStale = await allFiles();
  await dialog()
    .getByRole("button", { name: "强制覆盖并使用", exact: true })
    .click();
  await dialog()
    .getByRole("alert")
    .filter({ hasText: "确认期间配置又发生变化" })
    .waitFor();
  assert.deepEqual(await allFiles(), beforeStale);
  assert.equal((await status("codex")).activeProviderId, openaiB.id);
  await audit("Changed while confirming");
  await dialog()
    .getByRole("button", { name: "强制覆盖并使用", exact: true })
    .click();
  await dismissRestart("codex");
  assert.equal((await status("codex")).activeProviderId, openaiA.id);
  assert.ok(
    (await readFile(external.file, "utf8")).includes(
      "changed-during-confirmation",
    ),
  );
  checks.push(
    "确认窗口期间外部再次修改会停止写入、更新确认信息；只有再次点击确认才覆盖，保留最新无关设置",
  );
  assert.deepEqual(errors, []);
  const result = { root, checks, audits, pageErrors: errors };
  await writeFile(
    path.join(root, "results.json"),
    JSON.stringify(result, null, 2),
  );
  await writeFile(
    path.resolve(".qa/config-overwrite/results.json"),
    JSON.stringify(result, null, 2),
  );
  console.log(JSON.stringify(result, null, 2));
} catch (error) {
  if (page)
    await page
      .screenshot({ path: path.join(root, "failure.png") })
      .catch(() => {});
  throw error;
} finally {
  if (browser) await browser.close();
  if (app && app.exitCode === null)
    await new Promise((resolve) => {
      app.once("exit", resolve);
      app.kill();
    });
  await new Promise((resolve) => server.close(resolve));
}
