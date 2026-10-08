import assert from "node:assert/strict";
import http from "node:http";
import os from "node:os";
import path from "node:path";
import { spawn } from "node:child_process";
import { createHash } from "node:crypto";
import { copyFile, mkdir, readFile, writeFile, rm } from "node:fs/promises";
import { chromium } from "playwright";
import AxeBuilder from "@axe-core/playwright";

const root = path.resolve(".qa/remote-update", String(Date.now()));
const home = path.join(root, "隔离用户 中文"),
  codex = path.join(home, ".codex"),
  cli = path.join(home, ".claude"),
  local = path.join(root, "Local");
await Promise.all(
  [codex, cli, local].map((p) => mkdir(p, { recursive: true })),
);
const config = path.join(codex, "config.toml");
await writeFile(config, "# isolated fixture\nmodel='original'\n");
const context = path.join(root, "context.json");
await writeFile(
  context,
  JSON.stringify({
    home,
    local,
    roaming: null,
    roots: [home],
    notes: [],
    hints: [],
  }),
);
const openMarker = path.join(root, "opened-release.txt"),
  installMarker = path.join(root, "installer.txt");
const installer = Buffer.alloc(1024 * 1024, 42);
installer.write("MZ");
const sha = (bytes) => createHash("sha256").update(bytes).digest("hex");
const assetName = "uni-switch_99.0.0_x64-setup.exe";
let mode = "normal";
const requests = [],
  checks = [],
  audits = [],
  errors = [];
const server = http.createServer((req, res) => {
  requests.push({
    url: req.url,
    auth: !!req.headers.authorization,
    key: !!req.headers["x-api-key"],
  });
  const send = (body, status = 200) => {
    res.writeHead(status, { "Content-Type": "application/json" });
    res.end(JSON.stringify(body));
  };
  if (req.url === "/latest") {
    const tag = mode === "changed" ? "v99.0.1" : "v99.0.0";
    return send({
      tag_name: tag,
      html_url: "https://github.com/example/uni-switch/releases/tag/" + tag,
      body: "隔离更新测试",
      assets: [
        {
          name: assetName,
          size: installer.length,
          browser_download_url:
            "https://github.com/example/uni-switch/releases/download/v99.0.0/" +
            assetName,
        },
        {
          name: "SHA256SUMS.txt",
          browser_download_url:
            "https://github.com/example/uni-switch/releases/download/v99.0.0/SHA256SUMS.txt",
        },
      ],
    });
  }
  if (req.url.endsWith("/SHA256SUMS.txt")) {
    res.writeHead(200, { "Content-Type": "text/plain" });
    return res.end(sha(installer) + "  " + assetName + "\n");
  }
  if (req.url.endsWith("/" + assetName)) {
    const bytes =
      mode === "corrupt" ? Buffer.alloc(installer.length, 0) : installer;
    res.writeHead(200, {
      "Content-Length": bytes.length,
      "Content-Type": "application/octet-stream",
    });
    let offset = 0;
    const timer = setInterval(() => {
      res.write(bytes.subarray(offset, offset + 32768));
      offset += 32768;
      if (offset >= bytes.length) {
        clearInterval(timer);
        res.end();
      }
    }, 30);
    res.on("close", () => clearInterval(timer));
    return;
  }
  if (req.url.endsWith("/models"))
    return send({ data: [{ id: "gpt-test", object: "model" }] });
  send({}, 404);
});
await new Promise((r) => server.listen(0, "127.0.0.1", r));
const origin = "http://127.0.0.1:" + server.address().port;
let app, browser, page, cachedDirectory;
const until = async (fn) => {
  const end = Date.now() + 25000;
  while (Date.now() < end) {
    if (await fn()) return;
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error("Remote update QA timeout");
};
const portOpen = async () => {
  try {
    return (await fetch("http://127.0.0.1:9223/json/version")).ok;
  } catch {
    return false;
  }
};
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
async function audit(label) {
  const result = await new AxeBuilder({ page }).analyze();
  const violations = result.violations.map((v) => ({
    id: v.id,
    targets: v.nodes.map((n) => n.target),
  }));
  audits.push({ label, violations });
  assert.deepEqual(violations, []);
}
async function fileExists(p) {
  try {
    await readFile(p);
    return true;
  } catch {
    return false;
  }
}
try {
  assert.equal(await portOpen(), false, "Native QA must run sequentially");
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
      CODEX_HOME: codex,
      CLAUDE_CONFIG_DIR: cli,
      LOCALAPPDATA: local,
      UNI_SWITCH_DATA_DIR: path.join(root, "data"),
      WEBVIEW2_USER_DATA_FOLDER: path.join(root, "webview"),
      UNI_SWITCH_QA_DISCOVERY_CONTEXT: context,
      UNI_SWITCH_QA_UPDATE_REPOSITORY: "example/uni-switch",
      UNI_SWITCH_QA_UPDATE_ENDPOINT: origin + "/latest",
      UNI_SWITCH_QA_UPDATE_ASSET_ORIGIN: origin,
      UNI_SWITCH_QA_UPDATE_OPEN_MARKER: openMarker,
      UNI_SWITCH_QA_UPDATE_INSTALL_MARKER: installMarker,
    },
  });
  await until(portOpen);
  browser = await chromium.connectOverCDP("http://127.0.0.1:9223");
  page = browser.contexts()[0].pages()[0];
  page.setDefaultTimeout(15000);
  page.on("pageerror", (e) => errors.push(e.message));
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.setViewportSize({ width: 1120, height: 780 });
  await page.getByRole("button", { name: "设置", exact: true }).waitFor();
  for (const [target, directory] of [
    ["codex", codex],
    ["claude_cli", cli],
    ["claude_desktop", local],
  ])
    await invoke("set_directory", { target, directory });
  await invoke("save_provider", {
    input: {
      id: null,
      family: "codex",
      name: "提示测试",
      baseUrl: origin + "/v1",
      apiKey: "synthetic-update-test-key",
      model: "gpt-test",
      authMode: "bearer",
      reasoningEffort: "xhigh",
      codexOptions: {
        upstreamProtocol: "openai",
        protocolDetectedAt: 1,
        fastMode: false,
        models: [
          {
            id: "gpt-test",
            enabled: true,
            contextWindow: 256000,
            reasoningEfforts: [],
          },
        ],
        modelsSyncedAt: 1,
      },
    },
  });
  await page.getByRole("button", { name: "刷新配置状态", exact: true }).click();
  await page.getByRole("button", { name: "使用", exact: true }).click();
  let dialog = page.getByRole("dialog", {
    name: "重启 Codex 使配置生效",
    exact: true,
  });
  await dialog.waitFor();
  await dialog.getByText("切换后请新开对话", { exact: true }).waitFor();
  assert.ok(
    (await dialog.textContent()).includes("即使重启 Codex 或恢复旧会话"),
  );
  assert.ok(
    (await dialog.textContent()).includes("供应商使用记录反映实际请求"),
  );
  await audit("New conversation reminder");
  await page.screenshot({ path: path.join(root, "conversation-reminder.png") });
  await dialog.getByRole("button", { name: "稍后重启", exact: true }).click();
  const before = await readFile(config, "utf8");
  checks.push(
    "真实配置写入后提示新开对话、恢复旧会话仍可能沿用旧模型与强度、使用记录说明；未重启真实客户端",
  );
  await page.locator(".app-update-entry").click();
  dialog = page.getByRole("dialog", { name: "软件更新", exact: true });
  await dialog.getByRole("button", { name: "远程更新", exact: true }).waitFor();
  assert.equal(
    requests.some((r) => r.url.endsWith(".exe")),
    false,
  );
  checks.push("启动及打开更新仅检测版本，不自动下载安装；两种更新方式每次可选");
  await audit("Update method choices");
  await page.screenshot({ path: path.join(root, "update-choices.png") });
  await dialog
    .getByRole("button", { name: "GitHub 手动下载", exact: true })
    .click();
  assert.equal(
    await readFile(openMarker, "utf8"),
    "https://github.com/example/uni-switch/releases/tag/v99.0.0",
  );
  await dialog.getByRole("button", { name: "远程更新", exact: true }).click();
  await until(
    async () =>
      (await invoke("get_app_update_download"))?.phase === "downloading",
  );
  await dialog.getByRole("progressbar").waitFor();
  assert.equal(
    await dialog.getByRole("button", { name: "关闭弹窗" }).isEnabled(),
    false,
  );
  await page.keyboard.press("Escape");
  assert.equal(await dialog.count(), 1);
  await audit("Download progress");
  await dialog.getByRole("button", { name: "取消更新", exact: true }).click();
  await dialog.getByRole("button", { name: "远程更新", exact: true }).waitFor();
  assert.equal((await invoke("get_app_update_download")).phase, "cancelled");
  assert.equal(await fileExists(installMarker), false);
  checks.push(
    "显示实际字节进度；下载时关闭与 Escape 不丢失操作；取消清理且可重新选择",
  );
  mode = "corrupt";
  await dialog.getByRole("button", { name: "远程更新", exact: true }).click();
  await dialog.getByRole("alert").waitFor();
  assert.ok(
    (await dialog.getByRole("alert").textContent()).includes("校验失败"),
  );
  assert.equal(await fileExists(installMarker), false);
  checks.push("损坏安装包被 SHA256 拒绝，没有启动安装");
  mode = "changed";
  await dialog.getByRole("button", { name: "远程更新", exact: true }).click();
  await dialog.getByText(/最新版本已变化/).waitFor();
  assert.equal((await invoke("get_app_update_download")).phase, "failed");
  checks.push("版本在确认后变化时要求重新检测与选择，不静默更换版本");
  mode = "normal";
  await dialog.getByRole("button", { name: "远程更新", exact: true }).click();
  await dialog.getByRole("button", { name: "安装更新", exact: true }).waitFor();
  assert.equal(await fileExists(installMarker), false);
  await audit("Verified install confirmation");
  await page.screenshot({ path: path.join(root, "update-ready.png") });
  await page.setViewportSize({ width: 390, height: 620 });
  assert.equal(
    await dialog.evaluate((el) => el.scrollWidth > el.clientWidth),
    false,
  );
  await audit("Narrow verified update");
  await page.screenshot({ path: path.join(root, "update-ready-narrow.png") });
  await dialog.getByRole("button", { name: "安装更新", exact: true }).click();
  await dialog.getByText(/隔离测试：安装包已校验/).waitFor();
  const cachedPath = await readFile(installMarker, "utf8");
  assert.equal(sha(await readFile(cachedPath)), sha(installer));
  cachedDirectory = path.dirname(cachedPath);
  checks.push(
    "下载完成仍待用户点击安装，原生命令再次校验；QA 只记录安装路径，不执行真实安装、不退出用户软件",
  );
  assert.equal(await readFile(config, "utf8"), before);
  assert.ok(
    requests
      .filter(
        (r) => r.url === "/latest" || r.url.includes("/releases/download/"),
      )
      .every((r) => !r.auth && !r.key),
  );
  assert.deepEqual(errors, []);
  const result = {
    root,
    checks,
    audits,
    pageErrors: errors,
    requests,
    limitation:
      "Isolated Windows WebView2 and loopback mock GitHub assets. Real installer execution is deliberately intercepted; macOS/Linux native installation is not tested here.",
  };
  await writeFile(
    path.join(root, "results.json"),
    JSON.stringify(result, null, 2),
  );
  await writeFile(
    path.resolve(".qa/remote-update/results.json"),
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
    await new Promise((r) => {
      app.once("exit", r);
      app.kill();
    });
  await new Promise((r) => server.close(r));
  if (cachedDirectory) {
    assert.equal(path.dirname(cachedDirectory), os.tmpdir());
    assert.ok(path.basename(cachedDirectory).startsWith("uni-switch-update-"));
    await rm(cachedDirectory, { recursive: true, force: true });
  }
}
