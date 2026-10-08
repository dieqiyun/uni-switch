import { spawn } from "node:child_process";

// Only invoke against an isolated CODEX_HOME and a local mock supplier.
export async function startIsolatedCodex(codexHome) {
  const binary =
    process.env.CODEX_QA_BIN ||
    "C:/Users/Administrator/AppData/Local/OpenAI/Codex/bin/8aaf1547b825b104/codex.exe";
  const env = {
    ...process.env,
    CODEX_HOME: codexHome,
    HOME: codexHome,
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
  function rejectAll(error) {
    for (const task of pending.values()) {
      clearTimeout(task.timer);
      task.reject(error);
    }
    pending.clear();
  }
  proc.on("error", rejectAll);
  proc.on("exit", (code) =>
    rejectAll(new Error("Isolated Codex exited " + code + ": " + stderr)),
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
        clearTimeout(task.timer);
        if (msg.error) task.reject(new Error(JSON.stringify(msg.error)));
        else task.resolve(msg.result);
      } else notifications.push(msg);
    }
  });
  const call = (method, params) =>
    new Promise((resolve, reject) => {
      const requestId = ++id;
      const timer = setTimeout(() => {
        pending.delete(requestId);
        reject(new Error("Codex RPC timed out: " + method + ": " + stderr));
      }, 25000);
      pending.set(requestId, { resolve, reject, timer });
      proc.stdin.write(
        JSON.stringify({ id: requestId, method, params }) + "\n",
      );
    });
  const close = async () => {
    rejectAll(new Error("Isolated Codex QA finished"));
    if (proc.exitCode === null)
      await new Promise((resolve) => {
        proc.once("exit", resolve);
        proc.kill();
      });
  };
  try {
    await call("initialize", {
      clientInfo: {
        name: "uni_switch_model_routing_qa",
        title: "uni-switch routing QA",
        version: "0.5.20",
      },
      capabilities: { experimentalApi: true },
    });
    proc.stdin.write(JSON.stringify({ method: "initialized" }) + "\n");
  } catch (error) {
    await close();
    throw error;
  }
  return {
    binary,
    call,
    close,
    notifications,
    startThread: (overrides = {}) =>
      call("thread/start", {
        cwd: codexHome,
        sandbox: "read-only",
        approvalPolicy: "never",
        ...overrides,
      }),
    async turn(threadId, overrides = {}) {
      const index = notifications.length;
      const start = await call("turn/start", {
        threadId,
        input: [{ type: "text", text: "Reply hello. Do not call tools." }],
        ...overrides,
      });
      const deadline = Date.now() + 25000;
      while (Date.now() < deadline) {
        const completed = notifications
          .slice(index)
          .find(
            (msg) =>
              msg.method === "turn/completed" &&
              msg.params.turn.id === start.turn.id,
          );
        if (completed) {
          if (completed.params.turn.status !== "completed")
            throw new Error(JSON.stringify(completed.params.turn));
          return completed.params.turn;
        }
        await new Promise((resolve) => setTimeout(resolve, 100));
      }
      throw new Error("Isolated Codex turn timed out: " + stderr);
    },
  };
}
