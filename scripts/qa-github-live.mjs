import assert from "node:assert/strict";
import path from "node:path";
import { spawn } from "node:child_process";
import { mkdir, readFile, writeFile, copyFile } from "node:fs/promises";
import { chromium } from "playwright";

const version = JSON.parse(await readFile("package.json", "utf8")).version;
const repository = JSON.parse(await readFile("release-config.json", "utf8")).githubRepository;
assert.ok(repository, "A published repository must be bound before live verification");
const root = path.resolve(".qa/github-live", String(Date.now()));
const home = path.join(root, "home");
const codex = path.join(home, ".codex");
const cli = path.join(home, ".claude");
const local = path.join(root, "local");
await Promise.all([codex, cli, local].map((dir) => mkdir(dir, { recursive: true })));
const originalConfig = "# isolated GitHub update verification\n";
await writeFile(path.join(codex, "config.toml"), originalConfig);
const contextFile = path.join(root, "context.json");
await writeFile(contextFile, JSON.stringify({ home, local, roaming: null, roots: [home], notes: [], hints: [] }));
const endpoint = "http://127.0.0.1:9223";
async function portOpen() {
  try { return (await fetch(`${endpoint}/json/version`, { signal: AbortSignal.timeout(1000) })).ok; }
  catch { return false; }
}
async function until(fn) {
  const deadline = Date.now() + 25000;
  while (Date.now() < deadline) {
    if (await fn()) return;
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error("Live GitHub desktop verification timed out");
}
let app, browser, page;
const pageErrors = [];
try {
  assert.equal(await portOpen(), false, "Isolated QA port must be unused");
  const executable = path.join(root, "uni-switch.exe");
  await copyFile(path.resolve("src-tauri/target/debug/uni-switch.exe"), executable);
  const env = {
    ...process.env,
    UNI_SWITCH_DATA_DIR: path.join(root, "data"),
    UNI_SWITCH_QA_DISCOVERY_CONTEXT: contextFile,
    WEBVIEW2_USER_DATA_FOLDER: path.join(root, "webview"),
    CODEX_HOME: codex,
    CLAUDE_CONFIG_DIR: cli,
    LOCALAPPDATA: local,
  };
  // Use the compile-time repository and real public GitHub endpoint.
  delete env.UNI_SWITCH_QA_UPDATE_REPOSITORY;
  delete env.UNI_SWITCH_QA_UPDATE_ENDPOINT;
  delete env.UNI_SWITCH_QA_UPDATE_OPEN_MARKER;
  app = spawn(executable, [], { windowsHide: true, stdio: "ignore", env });
  await until(portOpen);
  browser = await chromium.connectOverCDP(endpoint);
  page = browser.contexts()[0].pages()[0];
  page.setDefaultTimeout(25000);
  page.on("pageerror", (error) => pageErrors.push(error.message));
  await page.setViewportSize({ width: 1120, height: 780 });
  async function invoke(command) {
    return page.evaluate((name) => window.__TAURI_INTERNALS__.invoke(name), command);
  }
  const source = await invoke("get_update_source");
  assert.equal(source.repository, repository);
  assert.equal(source.currentVersion, version);
  await page.locator(".sidebar .app-update-entry").getByText("检查更新", { exact: true }).waitFor();
  await page.waitForFunction(() => document.querySelector(".app-update-entry")?.textContent?.includes("检查更新"));
  await page.screenshot({ path: path.join(root, "main-window.png") });
  await page.locator(".sidebar-bottom").screenshot({ path: path.join(root, "version-entry.png") });
  await page.locator(".sidebar .app-update-entry").click();
  const dialog = page.getByRole("dialog", { name: "软件更新", exact: true });
  await dialog.getByText("已是最新版本", { exact: true }).waitFor();
  await dialog.getByRole("button", { name: "检测更新", exact: true }).click();
  await dialog.getByRole("button", { name: "检测更新", exact: true }).waitFor({ state: "visible" });
  await dialog.getByText("已是最新版本", { exact: true }).waitFor();
  assert.equal(await dialog.getByRole("alert").count(), 0);
  const release = await invoke("check_app_update");
  assert.equal(release.repository, repository);
  assert.equal(release.currentVersion, version);
  assert.equal(release.latestVersion, version);
  assert.equal(release.available, false);
  assert.equal(release.releaseUrl, `https://github.com/${repository}/releases/tag/v${version}`);
  assert.equal(release.downloadUrl, `https://github.com/${repository}/releases/download/v${version}/uni-switch_${version}_x64-setup.exe`);
  assert.equal(await readFile(path.join(codex, "config.toml"), "utf8"), originalConfig);
  assert.deepEqual(pageErrors, []);
  const screenshot = path.join(root, "software-update.png");
  await page.screenshot({ path: screenshot });
  const result = {
    version, repository, root, source, release, screenshot, pageErrors,
    checks: [
      "Compiled update source matches the renamed GitHub repository",
      "Startup and manual desktop checks use the live public GitHub API",
      "Published version is current and resolves the actual installer asset",
      "Isolated client configuration is unchanged",
    ],
  };
  await writeFile(path.join(root, "results.json"), JSON.stringify(result, null, 2));
  await writeFile(path.resolve(".qa/github-live/results.json"), JSON.stringify(result, null, 2));
  console.log(JSON.stringify(result, null, 2));
} catch (error) {
  if (page) await page.screenshot({ path: path.join(root, "failure.png") }).catch(() => {});
  throw error;
} finally {
  if (browser) await browser.close();
  if (app && app.exitCode === null) {
    await new Promise((resolve) => { app.once("exit", resolve); app.kill(); });
  }
}
