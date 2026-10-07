import { spawn } from "node:child_process";
import { writeFile } from "node:fs/promises";
import assert from "node:assert/strict";
import { verifyDesktopReasoning } from "./qa-codex-desktop-reasoning.mjs";

export async function mockModelResponse(req, res, requests) {
  let body = "";
  for await (const chunk of req) body += chunk;
  const request = JSON.parse(body);
  requests.push({ model: request.model, path: req.url });
  const message = {
    id: "msg_context",
    type: "message",
    role: "assistant",
    status: "completed",
    content: [
      { type: "output_text", text: "Context verified.", annotations: [] },
    ],
  };
  const response = {
    id: "resp_context",
    object: "response",
    created_at: Math.floor(Date.now() / 1000),
    status: "completed",
    model: request.model,
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
      delta: "Context verified.",
    },
    {
      type: "response.output_text.done",
      item_id: message.id,
      output_index: 0,
      content_index: 0,
      text: "Context verified.",
    },
    { type: "response.output_item.done", output_index: 0, item: message },
    { type: "response.completed", response },
  ])
    res.write(`event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`);
  res.end();
}

export async function verifyCodexModels({
  codexHome,
  expected,
  output,
  listOnly = false,
}) {
  const binary =
    process.env.CODEX_QA_BIN ||
    "C:/Users/Administrator/AppData/Local/OpenAI/Codex/bin/8aaf1547b825b104/codex.exe";
  const env = {
    ...process.env,
    CODEX_HOME: codexHome,
    NO_PROXY: "localhost,127.0.0.1",
  };
  delete env.OPENAI_API_KEY;
  for (const name of Object.keys(env))
    if (/^(https?|all)_proxy$/i.test(name)) delete env[name];
  const proc = spawn(binary, ["app-server"], {
    env,
    cwd: codexHome,
    windowsHide: true,
  });
  const pending = new Map(),
    notifications = [];
  let id = 0,
    buffer = "",
    stderr = "";
  const rejectAll = (error) => {
    for (const { reject } of pending.values()) reject(error);
  };
  proc.on("error", rejectAll);
  proc.on("exit", (code) =>
    rejectAll(new Error(`Codex exited ${code}: ${stderr}`)),
  );
  proc.stderr.on("data", (chunk) => {
    stderr += chunk;
  });
  proc.stdout.on("data", (chunk) => {
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
      if (msg.id != null) {
        const task = pending.get(msg.id);
        if (!task) continue;
        pending.delete(msg.id);
        if (msg.error) task.reject(new Error(JSON.stringify(msg.error)));
        else task.resolve(msg.result);
      } else notifications.push(msg);
    }
  });
  const call = (method, params) =>
    new Promise((resolve, reject) => {
      const requestId = ++id;
      pending.set(requestId, { resolve, reject });
      proc.stdin.write(
        JSON.stringify({ id: requestId, method, params }) + "\n",
      );
    });
  const until = async (predicate) => {
    const end = Date.now() + 20000;
    while (Date.now() < end) {
      const value = predicate();
      if (value) return value;
      await new Promise((r) => setTimeout(r, 100));
    }
    throw Error(`Codex notification timeout: ${stderr}`);
  };
  const timeout = setTimeout(() => {
    rejectAll(new Error("Codex QA timeout"));
    proc.kill();
  }, 60000);
  try {
    await call("initialize", {
      clientInfo: {
        name: "uni_switch_qa",
        title: "uni-switch QA",
        version: "0.3.6",
      },
      capabilities: { experimentalApi: true },
    });
    proc.stdin.write(JSON.stringify({ method: "initialized" }) + "\n");
    const list = await call("model/list", { limit: 100, includeHidden: false });
    assert.deepEqual(
      list.data.map((m) => m.id).sort(),
      Object.keys(expected).sort(),
    );
    const config = await call("config/read", { includeLayers: false });
    // Catalog-only checks do not test reasoning menus: internal review models
    // can legitimately be filtered out of that menu even while present in the
    // app-server catalog. Verify the requested IDs and startup model directly.
    if (listOnly) {
      assert.ok(
        Object.hasOwn(expected, config.config.model),
        "Codex startup model is in the enabled catalog",
      );
      const result = {
        models: list.data,
        defaultModel: config.config.model,
        note: "Isolated real Codex app-server model/list and config/read; no inference request or running desktop menu used.",
      };
      await writeFile(output, JSON.stringify(result, null, 2));
      return result;
    }
    for (const model of list.data) {
      for (const effort of [
        "none",
        "minimal",
        "low",
        "medium",
        "high",
        "xhigh",
        "max",
        "ultra",
      ])
        assert.equal(
          model.supportedReasoningEfforts.filter(
            (e) => e.reasoningEffort === effort,
          ).length,
          1,
        );
    }
    const desktopReasoning = await verifyDesktopReasoning({
      models: list.data,
      config: config.config,
    });
    assert.equal(config.config.model_context_window, null);
    assert.equal(config.config.model_auto_compact_token_limit, null);
    const thread = await call("thread/start", {
      model: Object.keys(expected)[0],
      cwd: codexHome,
      ephemeral: true,
      sandbox: "read-only",
      approvalPolicy: "never",
    });
    const windows = [];
    for (const [model, context] of Object.entries(expected)) {
      const startIndex = notifications.length;
      const result = await call("turn/start", {
        threadId: thread.thread.id,
        model,
        input: [{ type: "text", text: "Reply hello. Do not use tools." }],
      });
      const completed = await until(() =>
        notifications
          .slice(startIndex)
          .find(
            (msg) =>
              msg.method === "turn/completed" &&
              msg.params.turn.id === result.turn.id,
          ),
      );
      assert.equal(completed.params.turn.status, "completed");
      const usage = await until(() =>
        notifications
          .slice(startIndex)
          .findLast(
            (msg) =>
              msg.method === "thread/tokenUsage/updated" &&
              msg.params.turnId === result.turn.id,
          ),
      );
      const window = usage.params.tokenUsage.modelContextWindow;
      assert.equal(
        window,
        Math.floor(context * 0.95),
        `Codex applies the per-model window with its 95% input headroom for ${model}`,
      );
      windows.push({
        model,
        configuredContext: context,
        actualUsableContext: window,
      });
    }
    const result = {
      binary,
      codexHome,
      models: list.data.map((m) => m.id),
      reasoningEfforts: list.data.map((m) => ({
        model: m.id,
        efforts: m.supportedReasoningEfforts,
      })),
      desktopReasoning,
      windows,
      notifications,
      limitation:
        "Real Codex app-server against an isolated local mock provider; no live desktop menu or real supplier used.",
    };
    await writeFile(output, JSON.stringify(result, null, 2));
    return result;
  } finally {
    clearTimeout(timeout);
    proc.kill();
  }
}
