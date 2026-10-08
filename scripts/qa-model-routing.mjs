import assert from "node:assert/strict";
import http from "node:http";
import path from "node:path";
import { spawn } from "node:child_process";
import { copyFile, mkdir, readFile, writeFile } from "node:fs/promises";
import { chromium } from "playwright";
import AxeBuilder from "@axe-core/playwright";
import { mockModelResponse } from "./qa-codex-model-runtime.mjs";
import { startIsolatedCodex } from "./qa-codex-request-parameters.mjs";

const original = process.argv.includes("--expect-original");
const root = path.resolve(".qa/model-routing", String(Date.now()));
const home = path.join(root, "隔离用户 中文"),
  codex = path.join(home, ".codex"),
  local = path.join(root, "Local"),
  cli = path.join(home, ".claude");
await Promise.all(
  [codex, local, cli].map((p) => mkdir(p, { recursive: true })),
);
const configPath = path.join(codex, "config.toml");
await writeFile(
  configPath,
  "# isolated model routing fixture\nmodel='original'\n",
);
const contextFile = path.join(root, "context.json");
await writeFile(
  contextFile,
  JSON.stringify({
    home,
    local,
    roaming: null,
    roots: [home],
    notes: [],
    hints: [],
  }),
);
const key = "synthetic-model-routing-key",
  models = ["gpt-6.1-sol", "gpt-5.6-terra"],
  requests = [],
  checks = [],
  errors = [],
  audits = [];
const server = http.createServer((req, res) => {
  const send = (body, code = 200) => {
    res.writeHead(code, { "Content-Type": "application/json" });
    res.end(JSON.stringify(body));
  };
  if (req.method === "POST" && req.url.endsWith("/responses"))
    return void mockModelResponse(req, res, requests);
  if (req.url === "/github/releases/latest") return send({}, 404);
  if (req.url.endsWith("/models"))
    return send({
      object: "list",
      data: models.map((id) => ({ id, object: "model" })),
    });
  if (req.url.endsWith("/usage"))
    return send({ balance: 9, unit: "USD", mode: "unrestricted" });
  send({}, 404);
});
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const origin = "http://127.0.0.1:" + server.address().port;
let app, browser, page, oldCodex, freshCodex;
async function until(fn) {
  const end = Date.now() + 25000;
  while (Date.now() < end) {
    if (await fn()) return;
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error("Model routing QA timeout");
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
async function refresh() {
  const button = page.getByRole("button", {
    name: "刷新配置状态",
    exact: true,
  });
  await until(() => button.isEnabled());
  await button.click();
  await until(() => button.isEnabled());
}
async function dismissRestart() {
  const dialog = page.getByRole("dialog", {
    name: "重启 Codex 使配置生效",
    exact: true,
  });
  await dialog.waitFor();
  await dialog.getByRole("button", { name: "稍后重启", exact: true }).click();
  await dialog.waitFor({ state: "hidden" });
}
async function chooseFile(model, effort) {
  let config = await readFile(configPath, "utf8");
  config = config.replace(/^model\s*=.*$/m, 'model = "' + model + '"');
  config = config.replace(/^model_reasoning_effort\s*=.*\r?\n/gm, "");
  config = 'model_reasoning_effort = "' + effort + '"\n' + config;
  await writeFile(configPath, config);
}
function lastRequest(model, effort) {
  const req = requests.at(-1);
  assert.equal(req.model, model);
  assert.equal(req.reasoning?.effort ?? null, effort);
  return { model: req.model, effort: req.reasoning?.effort ?? null };
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
try {
  assert.equal(await portOpen(), false, "QA port must be free");
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
      UNI_SWITCH_QA_DISCOVERY_CONTEXT: contextFile,
      WEBVIEW2_USER_DATA_FOLDER: path.join(root, "webview"),
      CODEX_HOME: codex,
      CLAUDE_CONFIG_DIR: cli,
      LOCALAPPDATA: local,
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
    ["claude_desktop", local],
  ])
    await invoke("set_directory", { target, directory });
  const provider = await invoke("save_provider", {
    input: {
      id: null,
      family: "codex",
      name: "请求参数验证",
      baseUrl: origin + "/v1",
      apiKey: key,
      model: models[0],
      authMode: "bearer",
      reasoningEffort: null,
      codexOptions: {
        upstreamProtocol: "openai",
        protocolDetectedAt: Date.now(),
        models: models.map((id) => ({
          id,
          enabled: true,
          contextWindow: 256000,
          reasoningEfforts: [],
        })),
        fastMode: false,
      },
    },
  });
  await refresh();
  const row = page.locator(".provider-card").filter({
    has: page.getByRole("button", {
      name: "修改名称 请求参数验证",
      exact: true,
      includeHidden: true,
    }),
  });
  await row.getByRole("button", { name: "使用", exact: true }).click();
  await dismissRestart();
  await chooseFile(models[1], "medium");
  await refresh();
  const beforeStatus = (await invoke("get_overview")).targets.find(
    (value) => value.target === "codex",
  );
  assert.equal(beforeStatus.state, "applied");
  assert.equal(beforeStatus.appliedModel, models[0]);
  if (!original) {
    assert.equal(beforeStatus.configuredModel, models[1]);
    assert.equal(beforeStatus.configuredReasoningEffort, "medium");
    await row.getByText(/Codex 配置当前选择/).waitFor();
    assert.equal(
      await row
        .getByRole("button", { name: "重新应用", exact: true })
        .isEnabled(),
      true,
    );
    await audit("Current Codex file differs from supplier default");
    for (const size of [
      { width: 1120, height: 780 },
      { width: 760, height: 600 },
      { width: 390, height: 620 },
    ]) {
      await page.setViewportSize(size);
      assert.equal(
        await row.evaluate((el) => el.scrollWidth > el.clientWidth),
        false,
      );
      await audit("Selection mismatch " + size.width);
    }
    await page.setViewportSize({ width: 1120, height: 780 });
    await page.screenshot({
      path: path.join(root, "selection-difference.png"),
    });
  }
  oldCodex = await startIsolatedCodex(codex);
  const oldThread = await oldCodex.startThread();
  await oldCodex.turn(oldThread.thread.id);
  const oldRequest = lastRequest(models[1], "medium");
  checks.push(
    "真实Codex按当前配置选择5.6-terra/medium发出请求；供应商默认6.1-sol不代表已有会话的实际模型",
  );
  await chooseFile(models[1], "xhigh");
  await refresh();
  if (original) {
    await invoke("apply_provider", {
      target: "codex",
      providerId: provider.id,
    });
  } else
    await row.getByRole("button", { name: "重新应用", exact: true }).click();
  await dismissRestart();
  const appliedConfig = await readFile(configPath, "utf8");
  assert.ok(appliedConfig.includes('model = "gpt-6.1-sol"'));
  assert.equal(
    appliedConfig.includes('model_reasoning_effort = "xhigh"'),
    !original,
  );
  await oldCodex.turn(oldThread.thread.id);
  const oldAfterApply = lastRequest(models[1], "medium");
  checks.push(
    "重新应用只写配置文件，旧运行会话继续请求5.6-terra/medium，未被静默改动",
  );
  await oldCodex.close();
  oldCodex = null;
  freshCodex = await startIsolatedCodex(codex);
  const freshThread = await freshCodex.startThread();
  await freshCodex.turn(freshThread.thread.id);
  const freshRequest = lastRequest(models[0], original ? null : "xhigh");
  checks.push(
    original
      ? "修复前复现：重新应用清除xhigh，真实Codex新会话使用6.1-sol但未发送reasoning.effort，由上游使用默认值"
      : "修复后：重新应用保留xhigh，真实Codex新会话使用6.1-sol/xhigh",
  );
  const resumed = await freshCodex.call("thread/resume", {
    threadId: oldThread.thread.id,
  });
  await freshCodex.turn(resumed.thread.id);
  const resumedRequest = lastRequest(models[1], "medium");
  checks.push(
    "真实Codex恢复旧会话仍保留旧模型与思考强度；仅重新启动或更改全局配置不等于更改会话选择",
  );
  await freshCodex.turn(resumed.thread.id, {
    model: models[0],
    effort: "xhigh",
  });
  const explicitRequest = lastRequest(models[0], "xhigh");
  checks.push(
    "同一旧会话显式选择6.1-sol/xhigh后，上游收到的模型和effort完全一致，没有被uni-switch改写",
  );
  assert.deepEqual(errors, []);
  const result = {
    root,
    original,
    checks,
    beforeStatus: {
      appliedModel: beforeStatus.appliedModel,
      configuredModel: beforeStatus.configuredModel ?? null,
      configuredReasoningEffort: beforeStatus.configuredReasoningEffort ?? null,
    },
    requests: {
      oldRequest,
      oldAfterApply,
      freshRequest,
      resumedRequest,
      explicitRequest,
    },
    audits,
    pageErrors: errors,
    limitation:
      "Isolated uni-switch and real Codex 0.160.0 with local mock provider; no user's profiles, processes, API keys, live supplier or paid inference used. Cannot identify the user's reported request without its own metadata.",
  };
  await writeFile(
    path.join(root, "results.json"),
    JSON.stringify(result, null, 2),
  );
  await writeFile(
    path.resolve(
      ".qa/model-routing/" +
        (original ? "results-before-fix.json" : "results.json"),
    ),
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
  if (oldCodex) await oldCodex.close();
  if (freshCodex) await freshCodex.close();
  if (browser) await browser.close();
  if (app && app.exitCode === null)
    await new Promise((resolve) => {
      app.once("exit", resolve);
      app.kill();
    });
  await new Promise((resolve) => server.close(resolve));
}
