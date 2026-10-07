import { chromium } from "playwright";
import AxeBuilder from "@axe-core/playwright";
import http from "node:http";
import { spawn } from "node:child_process";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import assert from "node:assert/strict";

const binary = process.env.UNI_SWITCH_CODEX_BIN;
if (!binary)
  throw new Error("设置 UNI_SWITCH_CODEX_BIN 为隔离验证使用的 Codex 程序");
const root = path.resolve(".qa/codex-options");
const codexHome = path.join(root, "codex-home");
await mkdir(codexHome, { recursive: true });
await mkdir("docs/screenshots", { recursive: true });
await writeFile(
  path.join(codexHome, "config.toml"),
  '# 原始配置\nmodel = "original"\n[mcp_servers.keep]\ncommand = "demo"\n',
);
const requests = [];
let failModels = false,
  failBalance = false;
const key = "isolated-options-test-key-3210";
const server = http.createServer(async (req, res) => {
  let body = "";
  for await (const chunk of req) body += chunk;
  const record = {
    path: req.url,
    keyMatches: req.headers.authorization === `Bearer ${key}`,
    body: body ? JSON.parse(body) : null,
  };
  requests.push(record);
  if (req.url === "/v1/models") {
    res.writeHead(failModels ? 403 : 200, {
      "Content-Type": "application/json",
    });
    res.end(
      JSON.stringify(
        failModels
          ? { error: "denied" }
          : {
              data: [
                {
                  id: "gateway-coder",
                  context_window: 128000,
                  reasoning_efforts: ["low", "high"],
                },
                {
                  id: "gateway-mini",
                  context_window: 64000,
                  reasoning_efforts: ["low", "high"],
                },
                { id: "text-embedding-test" },
              ],
            },
      ),
    );
    return;
  }
  if (req.url === "/balance") {
    res.writeHead(failBalance ? 401 : 200, {
      "Content-Type": "application/json",
    });
    res.end(
      JSON.stringify(
        failBalance ? { error: "denied" } : { data: { remaining: 23.45 } },
      ),
    );
    return;
  }
  if (req.url === "/v1/responses") {
    const message = {
      id: "msg_options",
      type: "message",
      role: "assistant",
      status: "completed",
      content: [
        { type: "output_text", text: "Options verified.", annotations: [] },
      ],
    };
    const response = {
      id: "resp_options",
      object: "response",
      created_at: Math.floor(Date.now() / 1000),
      status: "completed",
      model: record.body.model,
      output: [message],
      usage: { input_tokens: 10, output_tokens: 4, total_tokens: 14 },
    };
    res.writeHead(200, {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache",
    });
    for (const event of [
      {
        type: "response.created",
        response: { ...response, status: "in_progress", output: [] },
      },
      {
        type: "response.output_item.added",
        output_index: 0,
        item: { ...message, status: "in_progress", content: [] },
      },
      {
        type: "response.content_part.added",
        item_id: message.id,
        output_index: 0,
        content_index: 0,
        part: { type: "output_text", text: "", annotations: [] },
      },
      {
        type: "response.output_text.delta",
        item_id: message.id,
        output_index: 0,
        content_index: 0,
        delta: "Options verified.",
      },
      {
        type: "response.output_text.done",
        item_id: message.id,
        output_index: 0,
        content_index: 0,
        text: "Options verified.",
      },
      { type: "response.output_item.done", output_index: 0, item: message },
      { type: "response.completed", response },
    ])
      res.write(`event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`);
    res.end();
    return;
  }
  res.writeHead(404);
  res.end("{}");
});
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const baseUrl = `http://127.0.0.1:${server.address().port}/v1`;
const env = { ...process.env, CODEX_HOME: codexHome };
delete env.OPENAI_API_KEY;
for (const name of Object.keys(env))
  if (/^(https?|all|no)_proxy$/i.test(name)) delete env[name];
env.NO_PROXY = "localhost,127.0.0.1";
const browser = await chromium.connectOverCDP("http://127.0.0.1:9223");
const page = browser.contexts().flatMap((c) => c.pages())[0];
const errors = [];
page.on("pageerror", (e) => errors.push(e.message));
const checks = [],
  accessibility = [];
const settle = async () =>
  page.evaluate(async () => {
    await Promise.all(
      document
        .getAnimations()
        .filter((a) => a.effect?.getComputedTiming().iterations !== Infinity)
        .map((a) => a.finished.catch(() => {})),
    );
  });
const audit = async (name) => {
  await settle();
  const result = await new AxeBuilder({ page }).analyze();
  accessibility.push({
    name,
    violations: result.violations.map((v) => ({
      id: v.id,
      nodes: v.nodes.map((n) => n.failureSummary),
    })),
  });
  assert.deepEqual(result.violations, []);
};
const closeNotice = async () => {
  const button = page.getByRole("button", { name: "关闭提示" });
  if (await button.count()) await button.click();
};
const card = page
  .getByRole("listitem")
  .filter({
    has: page.getByRole("heading", { name: "模型网关（测试）", exact: true }),
  });

async function appServerModels() {
  const child = spawn(binary, ["app-server"], {
    env,
    cwd: codexHome,
    windowsHide: true,
  });
  let buffer = "",
    stderr = "",
    resolveList,
    rejectList;
  const ready = new Promise((resolve, reject) => {
    resolveList = resolve;
    rejectList = reject;
  });
  child.on("error", rejectList);
  child.stderr.on("data", (chunk) => (stderr += chunk));
  child.stdout.on("data", (chunk) => {
    buffer += chunk;
    const lines = buffer.split("\n");
    buffer = lines.pop();
    for (const line of lines) {
      let msg;
      try {
        msg = JSON.parse(line);
      } catch {
        continue;
      }
      if (msg.id === 1) {
        if (msg.error) {
          rejectList(new Error(JSON.stringify(msg.error)));
          continue;
        }
        child.stdin.write(JSON.stringify({ method: "initialized" }) + "\n");
        child.stdin.write(
          JSON.stringify({
            id: 2,
            method: "model/list",
            params: { limit: 100, includeHidden: false },
          }) + "\n",
        );
      }
      if (msg.id === 2)
        msg.error
          ? rejectList(new Error(JSON.stringify(msg.error)))
          : resolveList(msg.result);
    }
  });
  child.on("exit", (code) =>
    rejectList(new Error(`app-server exited ${code}: ${stderr}`)),
  );
  child.stdin.write(
    JSON.stringify({
      id: 1,
      method: "initialize",
      params: {
        clientInfo: {
          name: "uni_switch_qa",
          title: "uni-switch QA",
          version: "0.3.0",
        },
        capabilities: { experimentalApi: true },
      },
    }) + "\n",
  );
  const timeout = setTimeout(() => {
    rejectList(new Error(`model/list timed out: ${stderr}`));
    child.kill();
  }, 30000);
  try {
    return await ready;
  } finally {
    clearTimeout(timeout);
    child.kill();
    await writeFile(path.join(root, "app-server-stderr.txt"), stderr);
  }
}
async function inference(model) {
  const child = spawn(
    binary,
    [
      "exec",
      "--skip-git-repo-check",
      "--ephemeral",
      "--sandbox",
      "read-only",
      "--json",
      ...(model ? ["--model", model] : []),
      "Reply hello. Do not use tools.",
    ],
    { env, cwd: codexHome, windowsHide: true },
  );
  child.stdin.end();
  let output = "",
    stderr = "";
  child.stdout.on("data", (c) => (output += c));
  child.stderr.on("data", (c) => (stderr += c));
  const timeout = setTimeout(() => child.kill(), 45000);
  try {
    const exit = await new Promise((resolve, reject) => {
      child.on("exit", resolve);
      child.on("error", reject);
    });
    await writeFile(
      path.join(root, `cli-${model || "default"}.txt`),
      output + "\n" + stderr,
    );
    assert.equal(exit, 0, stderr);
    assert.ok(output.includes("Options verified."));
  } finally {
    clearTimeout(timeout);
  }
}
try {
  await page.getByRole("tab", { name: /^Codex/ }).click();
  await page.getByRole("button", { name: "设置", exact: true }).click();
  await page.getByRole("button", { name: "修改目录", exact: true }).click();
  await page.locator("#directory").fill(codexHome);
  await page.getByRole("button", { name: "保存目录" }).click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  const add = page.getByRole("button", { name: "添加配置", exact: true });
  await (
    (await add.count())
      ? add
      : page.getByRole("button", { name: "添加第一组配置" })
  ).click();
  await page.locator("#baseUrl").fill(baseUrl);
  await page.locator("#apiKey").fill(key);
  await page.getByRole("button", { name: "同步模型列表", exact: true }).click();
  await page.getByText(/已获取 3 个模型/).waitFor();
  assert.equal(await page.locator("#model").inputValue(), "gateway-coder");
  await page.getByRole("checkbox", { name: "text-embedding-test" }).uncheck();
  await page.locator(".form-advanced:not(.balance-options)>summary").click();
  await page.locator("#name").fill("模型网关（测试）");
  await page.locator("#fastMode").selectOption("on");
  await page.locator("#reasoningEffort").selectOption("high");
  await page.locator("#contextWindow").fill("128000");
  await page.locator("#autoCompactTokenLimit").fill("100000");
  await page.locator(".balance-options>summary").click();
  await page.locator("#balancePreset").selectOption("custom");
  await page.locator("#balancePath").fill("/balance");
  await page.locator("#balanceJsonPath").fill("data.remaining");
  await page.locator("#balanceUnit").fill("USD");
  await page.getByRole("button", { name: "查询余额", exact: true }).click();
  await page.getByText("23.45 USD", { exact: true }).waitFor();
  await audit("Codex 模型、性能与余额表单");
  await page.locator(".model-discovery").screenshot({ path: "docs/screenshots/codex-models.png" });
  await page.locator(".form-advanced:not(.balance-options)").screenshot({ path: "docs/screenshots/codex-performance.png" });
  await page.locator(".balance-options").screenshot({ path: "docs/screenshots/codex-balance-settings.png" });
  checks.push(
    "密钥模型同步、取消非推理模型、默认模型、Fast / 推理 / 上下文 / 余额设置",
  );
  await page.getByRole("button", { name: "保存并应用", exact: true }).click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  await card.getByRole("button", { name: "已应用", exact: true }).waitFor();
  const config = await readFile(path.join(codexHome, "config.toml"), "utf8");
  assert.ok(config.includes('service_tier = "priority"'));
  assert.ok(config.includes('model_reasoning_effort = "high"'));
  assert.ok(config.includes("model_context_window = 128000"));
  assert.ok(config.includes("model_auto_compact_token_limit = 100000"));
  assert.ok(config.includes("mcp_servers.keep"));
  const catalog = JSON.parse(
    await readFile(path.join(codexHome, "uni-switch-models.json"), "utf8"),
  );
  assert.deepEqual(
    catalog.models.map((m) => m.slug),
    ["gateway-coder", "gateway-mini"],
  );
  checks.push("配置与模型目录写入，排除未勾选模型，保留原 MCP");
  await card.getByRole("button", { name: /查询.*余额/ }).click();
  await card.getByText("23.45 USD", { exact: true }).waitFor();
  await closeNotice();
  await page.evaluate(() => document.activeElement?.blur());
  await settle();
  await page.screenshot({ path: "docs/screenshots/codex-options-list.png" });
  await audit("含余额与多模型信息的供应商列表");
  failBalance = true;
  await card.getByRole("button", { name: /刷新.*余额/ }).click();
  await card.getByRole("alert").filter({ hasText: "401" }).waitFor();
  assert.ok(await card.getByText(/上次结果/).count());
  failBalance = false;
  checks.push("行内余额、查询失败保留上次结果和权限提示");
  await page.getByRole("button", { name: "编辑 模型网关（测试）" }).click();
  failModels = true;
  await page.getByRole("button", { name: "同步模型列表", exact: true }).click();
  await page.getByRole("alert").filter({ hasText: "403" }).waitFor();
  assert.equal(await page.getByRole("checkbox").count(), 3);
  failModels = false;
  await page.keyboard.press("Escape");
  checks.push("编辑使用已保存密钥，同步失败保留旧模型和用户勾选");
  const list = await appServerModels();
  await writeFile(
    path.join(root, "codex-model-list.json"),
    JSON.stringify(list, null, 2),
  );
  const ids = list.data.map((m) => m.id);
  assert.deepEqual([...ids].sort(), ["gateway-coder", "gateway-mini"]);
  checks.push("真实 Codex 0.160.0 app-server model/list 仅返回所选供应商模型");
  await inference();
  await inference("gateway-mini");
  const inferenceRequests = requests.filter((r) => r.path === "/v1/responses");
  assert.equal(inferenceRequests.length, 2);
  assert.equal(inferenceRequests[0].body.model, "gateway-coder");
  assert.equal(inferenceRequests[1].body.model, "gateway-mini");
  for (const request of inferenceRequests) {
    assert.ok(request.keyMatches);
    assert.equal(request.body.service_tier, "priority");
    assert.equal(request.body.reasoning.effort, "high");
  }
  checks.push(
    "真实 Codex 默认和另选模型均请求供应商，Fast 档位与推理强度进入 Responses 请求",
  );
  assert.ok(requests.every((r) => r.keyMatches));
  assert.deepEqual(errors, []);
  await writeFile(
    path.join(root, "results.json"),
    JSON.stringify(
      {
        passed: checks.length,
        checks,
        accessibility,
        pageErrors: errors,
        requests,
        realProviderTested: false,
      },
      null,
      2,
    ),
  );
  console.log(
    JSON.stringify({ passed: checks.length, checks, accessibility }, null, 2),
  );
} finally {
  await browser.close();
  await new Promise((resolve) => server.close(resolve));
}
