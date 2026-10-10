import assert from "node:assert/strict";
import path from "node:path";
import net from "node:net";
import { spawn } from "node:child_process";
import { copyFile, mkdir, readFile, writeFile } from "node:fs/promises";
import { chromium } from "playwright";
import AxeBuilder from "@axe-core/playwright";
import { preview } from "vite";
const root = path.resolve(".qa/client-config", String(Date.now()));
const home = path.join(root, "home"),
  local = path.join(root, "Local"),
  roaming = path.join(root, "Roaming");
const clients = {
  codex: path.join(home, ".codex"),
  claude_cli: path.join(home, ".claude"),
  claude_desktop: local,
  zcode: path.join(home, ".zcode/v2"),
  dsh: path.join(home, ".dsh"),
  workbuddy: path.join(home, ".workbuddy"),
};
await Promise.all(
  [home, local, roaming, ...Object.values(clients)].map((p) =>
    mkdir(p, { recursive: true }),
  ),
);
const originals = {
  codex: "# 中文原配置\nmodel='original'\n",
  claude_cli: '{"env":{"KEEP":"qa"}}\n',
  dsh: "# 保留注释\n- id: unrelated\n  config: !!js |\n    ({ enabled: true })\n",
  workbuddy: '{"models":[{"id":"keep","other":true}]}\n',
};
await writeFile(
  path.join(clients.codex, "config.toml"),
  originals.codex,
  "utf8",
);
await writeFile(
  path.join(clients.claude_cli, "settings.json"),
  originals.claude_cli,
  "utf8",
);
await writeFile(
  path.join(clients.dsh, "cordis.patch.yml"),
  originals.dsh,
  "utf8",
);
await writeFile(
  path.join(clients.workbuddy, "models.json"),
  originals.workbuddy,
  "utf8",
);
const context = path.join(root, "discovery.json");
await writeFile(
  context,
  JSON.stringify({ home, local, roaming, roots: [home], notes: [], hints: [] }),
  "utf8",
);
async function portOpen() {
  return new Promise((resolve) => {
    const socket = net.connect({ host: "127.0.0.1", port: 9223 });
    socket.once("connect", () => {
      socket.destroy();
      resolve(true);
    });
    socket.once("error", () => resolve(false));
  });
}
let app, browser, page, previewServer;
let output = "";
const errors = [];
const report = { root, checks: [], pageErrors: errors };
async function invoke(command, args = {}) {
  return page.evaluate(
    async ({ command, args }) =>
      window.__TAURI_INTERNALS__.invoke(command, args),
    { command, args },
  );
}
async function closeRestart(name) {
  await page
    .getByRole("heading", { name: name + " 配置已保存", exact: true })
    .waitFor();
  await page.getByRole("button", { name: "知道了", exact: true }).click();
}
try {
  assert.equal(
    await portOpen(),
    false,
    "Another QA session owns CDP 9223; leave it untouched",
  );
  previewServer = await preview({
    root: path.resolve("."),
    preview: { host: "127.0.0.1", port: 1420, strictPort: true },
    clearScreen: false,
  });
  const executable = path.join(root, "uni-switch.exe");
  await copyFile(
    path.resolve("src-tauri/target/debug/uni-switch.exe"),
    executable,
  );
  app = spawn(executable, [], {
    windowsHide: true,
    stdio: "pipe",
    env: {
      ...process.env,
      HOME: home,
      APPDATA: roaming,
      LOCALAPPDATA: local,
      CODEX_HOME: clients.codex,
      CLAUDE_CONFIG_DIR: clients.claude_cli,
      ZCODE_DATA_BASE_DIR: home,
      ZCODE_PERSONAL_PROVIDER_CONFIG_FILE: path.join(
        clients.zcode,
        "provider_config.json",
      ),
      DSH_HOME: clients.dsh,
      UNI_SWITCH_DATA_DIR: path.join(root, "data"),
      UNI_SWITCH_QA_DISCOVERY_CONTEXT: context,
      WEBVIEW2_USER_DATA_FOLDER: path.join(root, "webview"),
      UNI_SWITCH_QA_UPDATE_REPOSITORY: "example/uni-switch",
      UNI_SWITCH_QA_UPDATE_ENDPOINT: "http://127.0.0.1:1/no-updates",
    },
  });
  app.stdin.end();
  app.stdout.on("data", (s) => (output += s));
  app.stderr.on("data", (s) => (output += s));
  const deadline = Date.now() + 60000;
  while (!(await portOpen())) {
    assert.ok(Date.now() < deadline, "QA startup timeout: " + output);
    assert.equal(app.exitCode, null, "QA exited: " + output);
    await new Promise((r) => setTimeout(r, 200));
  }
  browser = await chromium.connectOverCDP("http://127.0.0.1:9223");
  page = browser.contexts()[0].pages()[0];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.getByRole("tab", { name: /ZCode/ }).waitFor();
  for (const [name, extension] of [
    ["ZCode", "png"],
    ["DSH", "svg"],
    ["WorkBuddy", "svg"],
  ]) {
    const image = page
      .getByRole("tab", { name: new RegExp(name) })
      .locator("img");
    await image.evaluate((img) => img.decode());
    assert.ok(
      await image.evaluate((img) => img.complete && img.naturalWidth > 0),
    );
    const src = await image.getAttribute("src");
    assert.ok(
      src.startsWith("/assets/") || src.startsWith("data:image/"),
      "brand must be bundled locally",
    );
    if (src.startsWith("/assets/"))
      assert.ok(src.split("?")[0].endsWith("." + extension));
  }
  report.checks.push(
    "ZCode / DSH / WorkBuddy official brand assets decode locally without broken images",
  );
  for (const [client, directory] of Object.entries(clients))
    await invoke("set_client_config_directory", { client, directory });
  const provider = await invoke("save_provider", {
    input: {
      id: null,
      family: "codex",
      name: "合成多客户端供应商",
      baseUrl: "https://gateway.example/gateway/v1",
      apiKey: "synthetic-client-config-key",
      model: "qa-model",
      authMode: "bearer",
      reasoningEffort: null,
      codexOptions: {
        upstreamProtocol: "openai",
        models: [
          {
            id: "qa-model",
            contextWindow: 128000,
            reasoningEfforts: [],
            enabled: true,
            profile: {
              endpoints: { chatCompletions: true, responses: true },
              toolCalls: true,
              maxOutputTokens: 8192,
            },
          },
        ],
      },
    },
  });
  await page.getByRole("button", { name: "刷新配置状态", exact: true }).click();
  for (const [client, name] of [
    ["zcode", "ZCode"],
    ["dsh", "DSH"],
    ["workbuddy", "WorkBuddy"],
  ]) {
    await page.getByRole("tab", { name: new RegExp(name) }).click();
    await page
      .getByRole("heading", { name: "一键配置 " + name, exact: true })
      .waitFor();
    await page.getByRole("button", { name: "一键配置", exact: true }).click();
    await closeRestart(name);
    const status = await invoke("get_client_config_status", { client });
    assert.equal(status.activeProviderId, provider.id);
    assert.equal(status.state, "applied");
    assert.ok(status.files.every((f) => f.path.startsWith(root)));
    const content = await readFile(status.files[0].path, "utf8");
    assert.ok(content.includes("qa-model"));
    if (client === "zcode") {
      const doc = JSON.parse(content);
      assert.equal(doc.config.defaultModelSelection.modelId, "qa-model");
      assert.equal(
        doc.config.providerConfigRules.providerRules[0].config.api.type,
        "openai-chat-completions",
      );
    }
    if (client === "dsh") {
      assert.ok(content.startsWith(originals.dsh));
      assert.ok(content.includes("uni-switch-llm"));
      const credential = JSON.parse(
        await readFile(status.files[1].path, "utf8"),
      );
      assert.equal(
        credential.refs.UNI_SWITCH_DSH_API_KEY,
        "synthetic-client-config-key",
      );
    }
    if (client === "workbuddy") {
      const doc = JSON.parse(content);
      assert.equal(doc.models[0].other, true);
      assert.equal(
        doc.models[1].url,
        "https://gateway.example/gateway/v1/chat/completions",
      );
    }
    await page.screenshot({ path: path.join(root, client + "-desktop.png") });
    await page
      .getByRole("button", { name: "查看 / 编辑配置文件", exact: true })
      .click();
    const editor = page.getByRole("dialog");
    await editor
      .getByRole("button", {
        name: new RegExp(
          client === "dsh"
            ? "cordis.patch.yml"
            : client === "zcode"
              ? "provider_config.json"
              : "models.json",
        ),
      })
      .click();
    const source = editor.getByRole("textbox");
    assert.equal(await source.getAttribute("readonly"), "");
    await editor.getByRole("button", { name: "开始编辑", exact: true }).click();
    await source.fill(content + "\n");
    await editor.getByRole("button", { name: "保存配置", exact: true }).click();
    await closeRestart(name);
    assert.equal(await readFile(status.files[0].path, "utf8"), content + "\n");
    await page.getByRole("button", { name: "一键配置", exact: true }).click();
    await page
      .getByRole("heading", { name: "确认覆盖配置", exact: true })
      .waitFor();
    await page.getByRole("button", { name: "取消", exact: true }).click();
    assert.equal(await readFile(status.files[0].path, "utf8"), content + "\n");
    await page.getByRole("button", { name: "一键配置", exact: true }).click();
    await page.getByRole("button", { name: "覆盖并应用", exact: true }).click();
    await closeRestart(name);
    const reapplied = await readFile(status.files[0].path, "utf8");
    if (client === "dsh") {
      assert.ok(reapplied.startsWith(originals.dsh));
      assert.equal(
        reapplied.replace(/\n+/g, "\n"),
        content.replace(/\n+/g, "\n"),
      );
    } else assert.equal(reapplied, content);
    report.checks.push(
      client +
        ": native config, source editor, restart prompt, cancelled conflict and confirmed overwrite",
    );
  }
  await page.getByRole("tab", { name: /Codex/ }).click();
  await page.getByRole("button", { name: "设置", exact: true }).click();
  await page
    .getByRole("button", { name: "查看 / 编辑配置文件", exact: true })
    .click();
  let editor = page.getByRole("dialog");
  await editor.getByRole("button", { name: /config.toml/ }).click();
  await editor.getByRole("button", { name: "开始编辑", exact: true }).click();
  await editor.getByRole("textbox").fill("# 手动配置\nmodel='manual-qa'\n");
  await editor.getByRole("button", { name: "保存配置", exact: true }).click();
  await closeRestart("Codex");
  assert.equal(
    await readFile(path.join(clients.codex, "config.toml"), "utf8"),
    "# 手动配置\nmodel='manual-qa'\n",
  );
  await page.getByRole("button", { name: "设置", exact: true }).click();
  await page
    .getByRole("button", { name: "查看 / 编辑配置文件", exact: true })
    .click();
  editor = page.getByRole("dialog");
  assert.ok(await editor.getByRole("button", { name: /auth.json/ }).count());
  await editor.getByRole("button", { name: /config.toml/ }).click();
  await editor.getByRole("button", { name: "开始编辑", exact: true }).click();
  await editor.getByRole("textbox").fill("model = [invalid");
  await editor.getByRole("button", { name: "保存配置", exact: true }).click();
  await editor.getByRole("alert").filter({ hasText: /TOML/ }).waitFor();
  assert.equal(
    await readFile(path.join(clients.codex, "config.toml"), "utf8"),
    "# 手动配置\nmodel='manual-qa'\n",
  );
  await page.screenshot({ path: path.join(root, "codex-editor-desktop.png") });
  await editor.getByRole("button", { name: "关闭", exact: true }).click();
  await editor
    .getByRole("button", { name: "放弃未保存修改", exact: true })
    .click();
  report.checks.push(
    "Codex: actual config and auth paths, TOML editor, manual save, format error does not write",
  );
  await page.getByRole("tab", { name: /Claude Code/ }).click();
  await page.getByRole("button", { name: "CLI", exact: true }).click();
  await page.getByRole("button", { name: "设置", exact: true }).click();
  await page
    .getByRole("button", { name: "查看 / 编辑配置文件", exact: true })
    .click();
  editor = page.getByRole("dialog");
  await editor.getByRole("button", { name: /settings.json/ }).click();
  await editor.getByRole("button", { name: "开始编辑", exact: true }).click();
  await editor
    .getByRole("textbox")
    .fill('{"env":{"KEEP":"qa"},"model":"manual-claude"}\n');
  await editor.getByRole("button", { name: "保存配置", exact: true }).click();
  await closeRestart("Claude Code CLI");
  report.checks.push(
    "Claude Code CLI: JSON editor and successful-save restart prompt",
  );
  const desktopLibrary = path.join(
    clients.claude_desktop,
    "Claude-3p/configLibrary",
  );
  const desktopId = "5cb1f4d8-2572-41de-80a1-b2e56570ba7e";
  await mkdir(desktopLibrary, { recursive: true });
  await writeFile(
    path.join(desktopLibrary, "_meta.json"),
    JSON.stringify({ appliedId: desktopId, entries: [] }),
    "utf8",
  );
  await writeFile(
    path.join(desktopLibrary, desktopId + ".json"),
    '{"inferenceProvider":"gateway","inferenceGatewayBaseUrl":"https://desktop.example"}',
    "utf8",
  );
  await page.getByRole("button", { name: "桌面端", exact: true }).click();
  await page.getByRole("button", { name: "设置", exact: true }).click();
  await page
    .getByRole("button", { name: "查看 / 编辑配置文件", exact: true })
    .click();
  editor = page.getByRole("dialog");
  await editor.getByRole("button", { name: new RegExp(desktopId) }).click();
  await editor.getByRole("button", { name: "开始编辑", exact: true }).click();
  await editor
    .getByRole("textbox")
    .fill(
      '{"inferenceProvider":"gateway","inferenceGatewayBaseUrl":"https://desktop.example","说明":"手动修改"}',
    );
  await editor.getByRole("button", { name: "保存配置", exact: true }).click();
  await closeRestart("Claude Code 桌面端");
  assert.equal(
    JSON.parse(
      await readFile(path.join(desktopLibrary, desktopId + ".json"), "utf8"),
    )["说明"],
    "手动修改",
  );
  report.checks.push(
    "Claude Code desktop: selected UUID profile inventory, editor and restart prompt",
  );

  await page.getByRole("tab", { name: /WorkBuddy/ }).click();
  const accessibility = await new AxeBuilder({ page }).analyze();
  assert.deepEqual(
    accessibility.violations.filter((v) =>
      ["serious", "critical"].includes(v.impact),
    ),
    [],
  );
  await page.setViewportSize({ width: 390, height: 844 });
  assert.ok(
    await page.evaluate(() =>
      [...document.querySelectorAll(".app-nav > button")].every((tab) => {
        const box = tab.getBoundingClientRect();
        const image = tab.querySelector("img").getBoundingClientRect();
        const name = tab.querySelector("strong").getBoundingClientRect();
        return (
          image.left >= box.left &&
          image.right <= name.left &&
          name.right <= box.right
        );
      }),
    ),
    "narrow client labels and official icons must fit inside each tab without overlap",
  );
  for (const name of ["ZCode", "DSH", "WorkBuddy"]) {
    await page.getByRole("tab", { name: new RegExp(name) }).click();
    await page
      .getByRole("heading", { name: "一键配置 " + name, exact: true })
      .waitFor();
    assert.ok(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth + 1,
      ),
    );
    await page.screenshot({
      path: path.join(root, name.toLowerCase() + "-narrow.png"),
    });
  }
  report.checks.push(
    "Three official icons render at desktop and 390px widths; no horizontal overflow or serious/critical accessibility findings",
  );
  if (process.argv.includes("--protocols")) {
    const fixtures = [];
    for (const client of ["zcode", "dsh", "workbuddy"]) {
      for (const protocol of client === "workbuddy"
        ? ["chat_completions"]
        : ["messages", "chat_completions", "responses"]) {
        const resource =
          protocol === "messages"
            ? "messages"
            : protocol === "responses"
              ? "responses"
              : "chat/completions";
        for (const auth of client === "workbuddy"
          ? ["bearer"]
          : ["bearer", "x-api-key"]) {
          for (const [pathCase, suffix, apiPath] of [
            ["origin", "", protocol === "messages" ? "" : "/v1"],
            ["v1", "/v1/", "/v1"],
            ["proxy-v1", "/proxy/v1", "/proxy/v1"],
            ["custom-v4", "/api/paas/v4", "/api/paas/v4"],
            ["custom-root", "/proxy", "/proxy"],
            ["full-endpoint", "/proxy/v1/" + resource + "/", "/proxy/v1"],
          ]) {
            await invoke("save_provider", {
              input: {
                id: provider.id,
                family: "codex",
                name: "合成原生协议供应商",
                baseUrl: "https://native-gateway.example" + suffix,
                apiKey: "synthetic-native-protocol-key",
                model: "qa-native-model",
                authMode: auth,
                reasoningEffort: null,
                codexOptions: {
                  upstreamProtocol:
                    protocol === "messages" ? "anthropic" : "openai",
                  models: [
                    {
                      id: "qa-native-model",
                      enabled: true,
                      contextWindow: 128000,
                      profileOverrides: {
                        endpoints: {
                          messages: true,
                          chatCompletions: true,
                          responses: true,
                        },
                        toolCalls: true,
                        maxOutputTokens: 8192,
                      },
                    },
                  ],
                },
              },
            });
            await invoke("apply_client_config", {
              client,
              providerId: provider.id,
              protocol,
            });
            const status = await invoke("get_client_config_status", { client });
            const contents = await Promise.all(
              status.files.map((f) => readFile(f.path, "utf8")),
            );
            const expectedEndpoint =
              "https://native-gateway.example" +
              (protocol === "messages" && !apiPath.endsWith("/v1")
                ? apiPath + "/v1"
                : apiPath) +
              "/" +
              resource;
            fixtures.push({
              client,
              protocol,
              auth,
              pathCase,
              expectedEndpoint,
              contents,
            });
          }
        }
      }
    }
    const status = await invoke("get_client_config_status", {
      client: "workbuddy",
    });
    const before = await readFile(status.files[0].path, "utf8");
    await invoke("save_provider", {
      input: {
        id: provider.id,
        family: "codex",
        name: "合成不兼容认证",
        baseUrl: "https://native-gateway.example/v1",
        apiKey: "synthetic-native-protocol-key",
        model: "qa-native-model",
        authMode: "x-api-key",
        reasoningEffort: null,
        codexOptions: { upstreamProtocol: "openai" },
      },
    });
    const failure = await page.evaluate(
      async ({ providerId }) => {
        try {
          await window.__TAURI_INTERNALS__.invoke("apply_client_config", {
            client: "workbuddy",
            providerId,
            protocol: "chat_completions",
          });
        } catch (e) {
          return { code: e.code, message: e.message };
        }
        return null;
      },
      { providerId: provider.id },
    );
    assert.equal(failure?.code, "unsupported_auth");
    assert.equal(await readFile(status.files[0].path, "utf8"), before);
    const filename = path.join(root, "protocol-fixtures.json");
    await writeFile(filename, JSON.stringify(fixtures, null, 2), "utf8");
    const { runNativeClientProtocols } = await import(
      "./qa-native-client-protocols.mjs"
    );
    report.nativeProtocols = await runNativeClientProtocols(fixtures);
    await writeFile(
      path.join(root, "native-protocol-report.json"),
      JSON.stringify(report.nativeProtocols, null, 2),
      "utf8",
    );
    report.checks.push(
      "WorkBuddy unsupported x-api-key is rejected before modifying existing files",
    );
  }
  assert.deepEqual(errors, []);
  await writeFile(
    path.join(root, "report.json"),
    JSON.stringify(report, null, 2),
    "utf8",
  );
  console.log(JSON.stringify(report, null, 2));
} finally {
  if (browser) await browser.close();
  if (app && app.exitCode === null) app.kill();
  if (previewServer) {
    previewServer.httpServer.closeAllConnections();
    await new Promise((r) => previewServer.httpServer.close(r));
  }
}
