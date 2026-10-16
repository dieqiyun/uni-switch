import assert from "node:assert/strict";
import http from "node:http";
import path from "node:path";
import { spawn } from "node:child_process";
import { mkdir, writeFile } from "node:fs/promises";

const directory = path.resolve(".qa/claude-55-audit", "cli-" + Date.now());
await mkdir(directory, { recursive: true });
const binary =
  process.env.UNI_SWITCH_CLAUDE_BIN ||
  (process.platform === "win32"
    ? path.join(
        process.env.APPDATA,
        "npm/node_modules/@anthropic-ai/claude-code/bin/claude.exe",
      )
    : "claude");
const checks = [];
for (const [requested, expected, tools] of [
  ["claude-opus-5-5", "claude-opus-5-5", false],
  ["claude-sonnet-5-5", "claude-sonnet-5-5", false],
  ["opus", "claude-opus-5-5", false],
  ["sonnet", "claude-sonnet-5-5", false],
  ["claude-opus-5-5", "claude-opus-5-5", true],
  ["claude-sonnet-5-5", "claude-sonnet-5-5", true],
]) {
  const label = requested + (tools ? "-tools" : "-text");
  const home = path.join(directory, label);
  await mkdir(home, { recursive: true });
  const fixture = path.join(home, "fixture.txt");
  await writeFile(fixture, "QA_READ_ONLY_FIXTURE", "utf8");
  const captured = [];
  const signature = "qa-synthetic-signature-" + expected;
  const key = "synthetic-claude-55-key";
  const reply = "QA_CLAUDE_55_OK";
  const server = http.createServer(async (request, response) => {
    try {
      let text = "";
      for await (const chunk of request) text += chunk;
      const body = text ? JSON.parse(text) : {};
      if (request.url.endsWith("/count_tokens")) {
        response.writeHead(200, { "content-type": "application/json" });
        return response.end(JSON.stringify({ input_tokens: 10 }));
      }
      assert.equal(
        new URL(request.url, "http://127.0.0.1").pathname,
        "/gateway/v1/messages",
      );
      assert.equal(request.headers.authorization, "Bearer " + key);
      assert.equal(body.model, expected);
      captured.push(body);
      const hasResult = body.messages?.some(
        (m) =>
          Array.isArray(m.content) &&
          m.content.some(
            (b) => b.type === "tool_result" && b.tool_use_id === "call_qa_read",
          ),
      );
      const callTool = tools && !hasResult;
      const content = callTool
        ? [
            { type: "thinking", thinking: "", signature },
            {
              type: "tool_use",
              id: "call_qa_read",
              name: "Read",
              input: { file_path: fixture },
            },
          ]
        : [{ type: "text", text: reply }];
      const message = {
        id: "msg_qa",
        type: "message",
        role: "assistant",
        model: expected,
        content,
        stop_reason: callTool ? "tool_use" : "end_turn",
        stop_sequence: null,
        usage: { input_tokens: 10, output_tokens: 5 },
      };
      if (!body.stream) {
        response.writeHead(200, { "content-type": "application/json" });
        return response.end(JSON.stringify(message));
      }
      response.writeHead(200, { "content-type": "text/event-stream" });
      const emit = (event) =>
        response.write(
          "event: " + event.type + "\ndata: " + JSON.stringify(event) + "\n\n",
        );
      emit({
        type: "message_start",
        message: {
          ...message,
          content: [],
          stop_reason: null,
          usage: { input_tokens: 10, output_tokens: 0 },
        },
      });
      content.forEach((block, index) => {
        if (block.type === "thinking") {
          emit({
            type: "content_block_start",
            index,
            content_block: { type: "thinking", thinking: "", signature: "" },
          });
          emit({
            type: "content_block_delta",
            index,
            delta: { type: "signature_delta", signature: block.signature },
          });
        } else if (block.type === "tool_use") {
          emit({
            type: "content_block_start",
            index,
            content_block: { ...block, input: {} },
          });
          emit({
            type: "content_block_delta",
            index,
            delta: {
              type: "input_json_delta",
              partial_json: JSON.stringify(block.input),
            },
          });
        } else {
          emit({
            type: "content_block_start",
            index,
            content_block: { type: "text", text: "" },
          });
          emit({
            type: "content_block_delta",
            index,
            delta: { type: "text_delta", text: reply },
          });
        }
        emit({ type: "content_block_stop", index });
      });
      emit({
        type: "message_delta",
        delta: { stop_reason: message.stop_reason, stop_sequence: null },
        usage: { output_tokens: 5 },
      });
      emit({ type: "message_stop" });
      response.end();
    } catch (error) {
      response.writeHead(400, { "content-type": "application/json" });
      response.end(
        JSON.stringify({
          type: "error",
          error: { type: "invalid_request_error", message: error.message },
        }),
      );
    }
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const settings = {
    env: {
      ANTHROPIC_BASE_URL:
        "http://127.0.0.1:" + server.address().port + "/gateway",
      ANTHROPIC_AUTH_TOKEN: key,
      ANTHROPIC_MODEL: expected,
      ANTHROPIC_DEFAULT_OPUS_MODEL: "claude-opus-5-5",
      ANTHROPIC_DEFAULT_SONNET_MODEL: "claude-sonnet-5-5",
      ANTHROPIC_DEFAULT_HAIKU_MODEL: "claude-haiku-5-5",
      ANTHROPIC_DEFAULT_FABLE_MODEL: "claude-fable-5-1",
    },
  };
  const settingsPath = path.join(home, "settings.json");
  await writeFile(settingsPath, JSON.stringify(settings), "utf8");
  const environment = { ...process.env };
  for (const name of Object.keys(environment))
    if (
      /^(ANTHROPIC_|CLAUDE_|AWS_|GOOGLE_|VERTEX_|FOUNDRY_|OPENAI_|CODEX_)/i.test(
        name,
      ) ||
      /^(https?|all|no)_proxy$/i.test(name)
    )
      delete environment[name];
  Object.assign(environment, {
    HOME: home,
    USERPROFILE: home,
    APPDATA: path.join(home, "Roaming"),
    LOCALAPPDATA: path.join(home, "Local"),
    TEMP: path.join(home, "Temp"),
    TMP: path.join(home, "Temp"),
    CODEX_HOME: path.join(home, ".codex"),
    CLAUDE_CONFIG_DIR: path.join(home, ".claude"),
    CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: "1",
    CLAUDE_CODE_SKIP_PROMPT_HISTORY: "1",
    DISABLE_TELEMETRY: "1",
    DISABLE_ERROR_REPORTING: "1",
    DISABLE_AUTOUPDATER: "1",
    NO_PROXY: "127.0.0.1,localhost",
    ...settings.env,
  });
  await Promise.all(
    ["Roaming", "Local", "Temp", ".claude", ".codex"].map((name) =>
      mkdir(path.join(home, name), { recursive: true }),
    ),
  );
  const args = [
    "--bare",
    "--print",
    "--model",
    requested,
    "--tools",
    tools ? "Read" : "",
    "--no-session-persistence",
    "--disable-slash-commands",
    "--strict-mcp-config",
    "--mcp-config",
    '{"mcpServers":{}}',
    "--setting-sources",
    "",
    "--settings",
    settingsPath,
    "--max-turns",
    "3",
  ];
  if (tools) args.push("--allowedTools", "Read");
  const prompt = tools
    ? "Read the fixture at " + fixture + " and then reply with QA_CLAUDE_55_OK."
    : "Reply with QA_CLAUDE_55_OK.";
  let child,
    output = "";
  try {
    child = spawn(binary, args, {
      cwd: home,
      env: environment,
      windowsHide: true,
      stdio: "pipe",
    });
    child.stdin.end(prompt);
    child.stdout.on("data", (chunk) => (output += chunk));
    child.stderr.on("data", (chunk) => (output += chunk));
    const timer = setTimeout(() => child.kill(), 45000);
    let code;
    try {
      code = await new Promise((resolve, reject) => {
        child.once("error", reject);
        child.once("exit", resolve);
      });
    } finally {
      clearTimeout(timer);
    }
    assert.equal(code, 0, output);
    assert.ok(output.includes(reply), output);
    assert.ok(captured.length > 0);
    if (tools) {
      const second = captured.find((body) =>
        body.messages?.some(
          (m) =>
            Array.isArray(m.content) &&
            m.content.some(
              (b) =>
                b.type === "tool_result" && b.tool_use_id === "call_qa_read",
            ),
        ),
      );
      assert.ok(second, "CLI must return the synthetic Read result");
      const replay = second.messages
        .flatMap((m) => (Array.isArray(m.content) ? m.content : []))
        .find((b) => b.type === "thinking");
      assert.deepEqual(replay, { type: "thinking", thinking: "", signature });
    }
    checks.push({
      requested,
      expected,
      tools,
      requests: captured.length,
      state: "passed",
    });
  } catch (error) {
    checks.push({
      requested,
      expected,
      tools,
      state: "failed",
      error: error.message,
      output,
      captured,
    });
  } finally {
    if (child && child.exitCode === null && child.signalCode === null)
      child.kill();
    server.closeAllConnections();
    await new Promise((resolve) => server.close(resolve));
  }
  await writeFile(
    path.join(directory, "report.json"),
    JSON.stringify({ checks }, null, 2) + "\n",
    "utf8",
  );
  console.log(JSON.stringify(checks.at(-1)));
}
assert.ok(checks.every((check) => check.state === "passed"));
console.log("Report: " + path.join(directory, "report.json"));
