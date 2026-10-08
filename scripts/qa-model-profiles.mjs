import assert from "node:assert/strict";
import http from "node:http";
import path from "node:path";
import { spawn } from "node:child_process";
import {
  mkdir,
  readFile,
  writeFile,
  copyFile,
  readdir,
} from "node:fs/promises";
import { chromium } from "playwright";
import AxeBuilder from "@axe-core/playwright";

const root = path.resolve(".qa/model-profiles", String(Date.now()));
const home = path.join(root, "isolated-home");
const codex = path.join(home, ".codex");
const cli = path.join(home, ".claude");
const local = path.join(root, "Local");
await Promise.all(
  [codex, cli, local].map((directory) => mkdir(directory, { recursive: true })),
);
const original = "# isolated synthetic client\nmodel='original'\n";
await writeFile(path.join(codex, "config.toml"), original, "utf8");
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
  "utf8",
);
const key = "synthetic-model-profiles-key";
const model = "claude-haiku-5-5";
const inferences = [];
const server = http.createServer(async (request, response) => {
  const send = (value, status = 200) => {
    response.writeHead(status, { "content-type": "application/json" });
    response.end(JSON.stringify(value));
  };
  if (request.url.endsWith("/models")) {
    assert.equal(request.headers.authorization, `Bearer ${key}`);
    return send({
      object: "list",
      data: [
        {
          id: model,
          object: "model",
          supported_endpoint_types: ["anthropic", "openai"],
          context_window: 1000000,
          max_output_tokens: 128000,
          capabilities: {
            thinking: { types: { adaptive: { supported: true } } },
            effort: {
              supported_levels: ["low", "medium", "high", "xhigh", "max"],
              default_level: "medium",
            },
            sampling_parameters: { supported: false },
          },
        },
      ],
    });
  }
  if (request.method === "POST") {
    assert.equal(request.headers.authorization, `Bearer ${key}`);
    let text = "";
    for await (const chunk of request) text += chunk;
    const body = JSON.parse(text);
    inferences.push({ path: request.url, body });
    assert.equal(
      body[
        request.url.endsWith("responses") ? "max_output_tokens" : "max_tokens"
      ],
      256,
    );
    if (request.url.endsWith("responses"))
      return send(
        { error: { message: "not implemented; synthetic-model-profiles-key" } },
        500,
      );
    return send({
      content: [{ type: "text", text: "red" }],
      stop_reason: "end_turn",
    });
  }
  send({}, 404);
});
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const baseUrl = `http://127.0.0.1:${server.address().port}/v1`;
let app, browser, page, devServer;
const errors = [],
  audits = [];
const checks = [];
const portOpen = async () => {
  try {
    return (await fetch("http://127.0.0.1:9223/json/version")).ok;
  } catch {
    return false;
  }
};
async function until(check) {
  const deadline = Date.now() + 25000;
  while (Date.now() < deadline) {
    if (await check()) return;
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  throw new Error("QA startup timed out");
}
const invoke = (command, args) =>
  page.evaluate(
    ({ command, args }) => window.__TAURI_INTERNALS__.invoke(command, args),
    { command, args },
  );
async function audit(label) {
  const result = await new AxeBuilder({ page }).analyze();
  const violations = result.violations.map((entry) => ({
    id: entry.id,
    targets: entry.nodes.map((node) => node.target),
  }));
  audits.push({ label, violations });
  assert.deepEqual(violations, []);
}
try {
  assert.equal(
    await portOpen(),
    false,
    "Do not interfere with an existing CDP/native QA process",
  );
  devServer = await (
    await import("vite")
  ).preview({
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
    stdio: "ignore",
    env: {
      ...process.env,
      HOME: home,
      UNI_SWITCH_DATA_DIR: path.join(root, "data"),
      UNI_SWITCH_QA_DISCOVERY_CONTEXT: context,
      WEBVIEW2_USER_DATA_FOLDER: path.join(root, "webview"),
      CODEX_HOME: codex,
      CLAUDE_CONFIG_DIR: cli,
      LOCALAPPDATA: local,
      UNI_SWITCH_QA_UPDATE_REPOSITORY: "example/uni-switch",
      UNI_SWITCH_QA_UPDATE_ENDPOINT: baseUrl + "/github/releases/latest",
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
    ["claude_desktop", local],
  ])
    await invoke("set_directory", { target, directory });
  const provider = await invoke("save_provider", {
    input: {
      id: null,
      family: "codex",
      name: "模型档案 QA",
      baseUrl,
      apiKey: key,
      model,
      authMode: "bearer",
      reasoningEffort: null,
      codexOptions: {
        upstreamProtocol: "anthropic",
        protocolDetectedAt: 1,
        models: [
          {
            id: model,
            enabled: true,
            contextWindow: 256000,
            reasoningEfforts: [],
          },
        ],
      },
    },
  });
  await page.reload();
  const baseline = (await invoke("get_overview")).providers;
  await assert.rejects(
    invoke("verify_model_connection", {
      input: { providerId: provider.id, baseUrl, apiKey: null },
      model,
      authMode: "bearer",
      endpoint: "messages",
      feature: "image",
      consent: false,
    }),
  );
  assert.equal(inferences.length, 0);
  checks.push(
    "backend refuses inference before consent and without touching client files",
  );
  await page
    .getByRole("button", { name: "配置 模型档案 QA 的模型", exact: true })
    .click();
  const dialog = page.getByRole("dialog", {
    name: "模型档案 QA · 模型配置",
    exact: true,
  });
  await dialog.getByText("已选择 1 / 1", { exact: true }).waitFor();
  assert.equal(await dialog.getByRole("combobox").count(), 0);
  assert.equal(
    await dialog
      .getByLabel(`上下文长度 ${model}`, { exact: true })
      .inputValue(),
    "256",
  );
  assert.equal(inferences.length, 0);
  await dialog.locator("summary").click();
  await dialog.getByText(/Responses：明确不支持/).waitFor();
  const thinking = dialog.getByLabel(`转换请求的思考模式（${model}）`, {
    exact: true,
  });
  assert.equal(await thinking.inputValue(), "auto");
  await thinking.selectOption("budget");
  await dialog
    .getByRole("button", { name: "刷新模型列表", exact: true })
    .click();
  await dialog
    .getByRole("button", { name: "刷新模型列表", exact: true })
    .isEnabled();
  await until(
    async () =>
      (await thinking.inputValue()) === "budget" &&
      !(await dialog.getByText("正在更新模型列表…", { exact: true }).count()),
  );
  await dialog
    .getByRole("button", { name: `恢复自动能力 ${model}`, exact: true })
    .click();
  assert.equal(await thinking.inputValue(), "auto");
  checks.push(
    "OpenAI-shaped models honor explicit Anthropic endpoint metadata; manual thinking survives sync and reset works; context stays 256K",
  );
  await dialog.getByLabel("验证项目", { exact: true }).selectOption("image");
  assert.equal(
    await dialog
      .getByRole("button", { name: "开始验证", exact: true })
      .isEnabled(),
    false,
  );
  await dialog
    .getByRole("checkbox", {
      name: "我确认发送验证请求并接受可能的费用",
      exact: true,
    })
    .check();
  await dialog.getByRole("button", { name: "开始验证", exact: true }).click();
  await dialog
    .getByRole("status")
    .filter({ hasText: "本次合成请求验证通过" })
    .waitFor();
  assert.equal(inferences.length, 1);
  assert.equal(inferences[0].path, "/v1/messages");
  assert.equal(inferences[0].body.messages[0].content[1].type, "image");
  await dialog
    .getByLabel("验证接口", { exact: true })
    .selectOption("responses");
  assert.equal(
    await dialog
      .getByRole("checkbox", {
        name: "我确认发送验证请求并接受可能的费用",
        exact: true,
      })
      .isChecked(),
    false,
  );
  await dialog
    .getByRole("checkbox", {
      name: "我确认发送验证请求并接受可能的费用",
      exact: true,
    })
    .check();
  await dialog.getByRole("button", { name: "开始验证", exact: true }).click();
  await dialog.getByRole("status").filter({ hasText: "HTTP 500" }).waitFor();
  assert.equal(inferences.length, 2);
  assert.equal((await dialog.innerText()).includes(key), false);
  checks.push(
    "one synthetic image request per consent; 500 is unknown without retries, key leaks or permanent disablement",
  );
  for (const size of [
    { width: 1120, height: 780 },
    { width: 390, height: 620 },
  ]) {
    await page.setViewportSize(size);
    assert.equal(
      await dialog.evaluate(
        (element) => element.scrollWidth > element.clientWidth,
      ),
      false,
    );
    await audit(`expanded model profile ${size.width}px`);
    await page.screenshot({
      path: path.join(root, `profile-${size.width}.png`),
    });
  }
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  assert.deepEqual((await invoke("get_overview")).providers, baseline);
  assert.equal(
    await readFile(path.join(codex, "config.toml"), "utf8"),
    original,
  );
  const observations = await readdir(
    path.join(root, "data", "model-verifications"),
  );
  assert.equal(observations.length, 2);
  for (const filename of observations)
    assert.equal(
      (
        await readFile(
          path.join(root, "data", "model-verifications", filename),
          "utf8",
        )
      ).includes(key),
      false,
    );
  await page.setViewportSize({ width: 1120, height: 780 });
  await page.getByRole("button", { name: "设置", exact: true }).click();
  await page.getByText(/发布者尚未配置签名更新源/).waitFor();
  assert.equal(
    await page
      .getByRole("button", { name: "更新模型资料", exact: true })
      .isEnabled(),
    false,
  );
  await audit("registry settings without deployed feed");
  assert.deepEqual(errors, []);
  checks.push(
    "cancel preserves database choices and all isolated client config; observations contain no keys; no signed feed falsely claimed",
  );
  const result = {
    root,
    checks,
    audits,
    pageErrors: errors,
    inferenceCount: inferences.length,
    limitation:
      "isolated native uni-switch, synthetic local providers; no real client processes, credentials or paid inference",
  };
  await writeFile(
    path.join(root, "results.json"),
    JSON.stringify(result, null, 2),
  );
  await writeFile(
    path.resolve(".qa/model-profiles/results.json"),
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
  if (devServer) {
    devServer.httpServer.closeAllConnections();
    await new Promise((resolve) => devServer.httpServer.close(resolve));
  }
  server.closeAllConnections();
  await new Promise((resolve) => server.close(resolve));
}
