import http from "node:http";
import { spawn } from "node:child_process";
import { mkdir, readFile, readdir, writeFile } from "node:fs/promises";
import path from "node:path";
import assert from "node:assert/strict";

const binary = process.env.UNI_SWITCH_CODEX_BIN;
if (!binary)
  throw new Error("设置 UNI_SWITCH_CODEX_BIN 为要验证的 Codex 可执行文件路径");
const dataDirectory = process.env.UNI_SWITCH_QA_DATA_DIR || ".qa/native-data2";
const outputDirectory = process.env.UNI_SWITCH_QA_OUTPUT_DIR || ".qa";
await mkdir(outputDirectory, { recursive: true });
const operations = await readdir(path.join(dataDirectory, "backups"));
let source;
for (const name of operations) {
  const operation = JSON.parse(
    await readFile(path.join(dataDirectory, "backups", name), "utf8"),
  );
  if (operation.target === "codex" && operation.active)
    source = operation.changes[0].after;
}
assert.ok(source, "先运行真实桌面 QA，获取后端生成的 Codex 配置");
const requests = [];
const server = http.createServer(async (req, res) => {
  let text = "";
  for await (const chunk of req) text += chunk;
  requests.push({
    url: req.url,
    authorization: req.headers.authorization,
    body: text ? JSON.parse(text) : null,
  });
  if (!req.url.includes("/responses")) {
    res.writeHead(404);
    res.end("{}");
    return;
  }
  const message = {
    id: "msg_test",
    type: "message",
    role: "assistant",
    status: "completed",
    content: [
      { type: "output_text", text: "Mock API connected.", annotations: [] },
    ],
  };
  const response = {
    id: "resp_test",
    object: "response",
    created_at: Math.floor(Date.now() / 1000),
    status: "completed",
    model: "test-model",
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
      item_id: "msg_test",
      output_index: 0,
      content_index: 0,
      part: { type: "output_text", text: "", annotations: [] },
    },
    {
      type: "response.output_text.delta",
      item_id: "msg_test",
      output_index: 0,
      content_index: 0,
      delta: "Mock API connected.",
    },
    {
      type: "response.output_text.done",
      item_id: "msg_test",
      output_index: 0,
      content_index: 0,
      text: "Mock API connected.",
    },
    { type: "response.output_item.done", output_index: 0, item: message },
    { type: "response.completed", response },
  ])
    res.write(`event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`);
  res.end();
});
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const directory = path.resolve(outputDirectory, "codex-smoke");
await mkdir(directory, { recursive: true });
if (process.env.UNI_SWITCH_QA_AUTH_FILE) {
  await writeFile(
    path.join(directory, "auth.json"),
    await readFile(process.env.UNI_SWITCH_QA_AUTH_FILE),
  );
}
const port = server.address().port;
await writeFile(
  path.join(directory, "config.toml"),
  source.replace(
    "https://codex.example.test/v1",
    `http://127.0.0.1:${port}/v1`,
  ),
);
try {
  const env = { ...process.env, CODEX_HOME: directory };
  delete env.OPENAI_API_KEY;
  // Keep this isolated localhost test independent of the user's proxy settings.
  for (const key of Object.keys(env)) {
    if (/^(https?|all|no)_proxy$/i.test(key)) delete env[key];
  }
  env.NO_PROXY = "127.0.0.1,localhost";
  const child = spawn(
    binary,
    [
      "exec",
      "--skip-git-repo-check",
      "--ephemeral",
      "--sandbox",
      "read-only",
      "--json",
      "Reply hello. Do not use tools.",
    ],
    { env, cwd: directory, windowsHide: true },
  );
  child.stdin.end();
  let output = "";
  let stderr = "";
  child.stdout.on("data", (chunk) => (output += chunk));
  child.stderr.on("data", (chunk) => (stderr += chunk));
  const timeout = setTimeout(() => child.kill(), 45000);
  const exitCode = await new Promise((resolve, reject) => {
    child.on("error", reject);
    child.on("exit", resolve);
  });
  clearTimeout(timeout);
  await writeFile(
    path.join(outputDirectory, "codex-smoke-output.txt"),
    output + "\n" + stderr,
  );
  assert.equal(
    exitCode,
    0,
    "Codex 应使用应用生成的配置成功完成模拟请求，详情见 .qa/codex-smoke-output.txt",
  );
  const inference = requests.find((req) => req.url.includes("/responses"));
  assert.ok(inference);
  assert.equal(inference.authorization, "Bearer desktop-test-key-1234");
  assert.equal(inference.body.model, "test-model");
  assert.ok(output.includes("Mock API connected."));
  const result = {
    passed: true,
    requestPath: inference.url,
    model: inference.body.model,
    selectedKeyUsed: true,
    responseReceived: true,
    realProviderTested: false,
  };
  await writeFile(
    path.join(outputDirectory, "codex-cli-results.json"),
    JSON.stringify(result, null, 2),
  );
  console.log(JSON.stringify(result, null, 2));
} finally {
  await new Promise((resolve) => server.close(resolve));
}
