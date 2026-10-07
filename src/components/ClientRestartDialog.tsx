import { useState } from "react";
import { RotateCcw } from "lucide-react";
import { Modal } from "./Modal";
import { api, errorMessage } from "../lib/api";
import type { RuntimeStatus, Target } from "../types";

export const restartNames: Record<Target, string> = {
  codex: "Codex",
  claude_desktop: "Claude Code 桌面端",
  claude_cli: "Claude CLI",
};
export function needsRestart(runtime: RuntimeStatus | undefined) {
  return (
    !!runtime &&
    (runtime.target === "claude_cli"
      ? runtime.clientRunning && runtime.restartRequired
      : runtime.desktopRunning && runtime.desktopRestartRequired)
  );
}
export function ClientRestartDialog({
  runtime,
  onLater,
  onRestarted,
}: {
  runtime: RuntimeStatus;
  onLater: () => void;
  onRestarted: (message: string) => void;
}) {
  const [restarting, setRestarting] = useState(false);
  const [failure, setFailure] = useState("");
  const name = restartNames[runtime.target];
  const cli = runtime.target === "claude_cli";
  const canRestart = runtime.canRestartClient ?? runtime.canRestartDesktop;
  return (
    <Modal
      title={`重启 ${name} 使配置生效`}
      description={
        runtime.target === "codex"
          ? "Codex 配置已更新。请重启已打开的 Codex，使本次修改生效。"
          : `新配置已保存。检测到 ${name} 仍在使用修改前的配置，重启后即可加载。`
      }
      onClose={onLater}
      busy={restarting}
      dismissOnOutside={false}
      initialFocusId="client-restart-later"
    >
      <p className="scope-hint">
        {cli
          ? "立即重启会打开一个接续终端。请在原 Claude CLI 输入 /exit，旧会话退出后会在原工作目录恢复会话并加载新配置。"
          : `立即重启会关闭并重新打开 ${name}，正在进行的任务会中断。也可以稍后自行重启。`}
      </p>
      {cli && (
        <p className="scope-hint">
          也可以稍后在原终端输入 <code>/exit</code>，再运行{" "}
          <code>claude --continue</code> 恢复最近会话，或运行{" "}
          <code>claude</code> 开始新会话。
        </p>
      )}
      {runtime.target === "codex" && (
        <p className="scope-hint">
          Codex CLI 请退出旧进程后重新运行。Codex
          尚未运行时，下次启动会读取新配置。
        </p>
      )}
      {runtime.restartInProgress && (
        <p role="status" className="scope-hint">
          接续终端已打开，正在等待原 Claude CLI 退出。
        </p>
      )}
      {canRestart === false && (
        <p className="scope-hint">
          {runtime.restartReason ||
            (runtime.target === "codex" && !runtime.desktopRunning
              ? "未检测到可自动重启的 Codex 桌面实例，请手动重启已打开的客户端。"
              : `暂时无法自动重启，请手动退出 ${name} 后重新打开。`)}
        </p>
      )}
      {failure && (
        <p className="error-notice" role="alert">
          {failure}
        </p>
      )}
      {restarting && (
        <p role="status" className="submit-progress">
          {cli ? "正在打开接续终端…" : `正在关闭并重新打开 ${name}…`}
        </p>
      )}
      <div className="modal-actions">
        <button
          id="client-restart-later"
          type="button"
          className="button secondary"
          disabled={restarting}
          onClick={onLater}
        >
          稍后重启
        </button>
        <button
          type="button"
          className="button primary"
          disabled={restarting || canRestart === false}
          onClick={async () => {
            setRestarting(true);
            setFailure("");
            try {
              const result =
                runtime.target === "codex"
                  ? await api.restartCodex(runtime.configurationRevision || 0)
                  : await api.restartClient(
                      runtime.target,
                      runtime.configurationRevision || 0,
                    );
              onRestarted(result.message);
            } catch (error) {
              setFailure(errorMessage(error));
            } finally {
              setRestarting(false);
            }
          }}
        >
          <RotateCcw size={16} aria-hidden />
          {restarting ? "正在重启…" : "立即重启"}
        </button>
      </div>
    </Modal>
  );
}
