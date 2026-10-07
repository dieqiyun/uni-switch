import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "../lib/api";
import { ClientRestartDialog } from "./ClientRestartDialog";
import type { RuntimeStatus, Target } from "../types";

const runtime = (target: Target): RuntimeStatus => ({
  target,
  clientRunning: true,
  restartRequired: true,
  bridgeRequired: false,
  bridgeHealthy: true,
  configurationRevision: 456,
  desktopRunning: target !== "claude_cli",
  desktopRestartRequired: target !== "claude_cli",
  canRestartClient: true,
});
afterEach(() => vi.restoreAllMocks());
describe("Claude 配置生效与重启选择", () => {
  it.each(["claude_desktop", "claude_cli"] as const)(
    "%s 稍后和Escape保持原客户端运行",
    async (target) => {
      const later = vi.fn(),
        restart = vi.spyOn(api, "restartClient");
      render(
        <ClientRestartDialog
          runtime={runtime(target)}
          onLater={later}
          onRestarted={vi.fn()}
        />,
      );
      expect(screen.getByRole("button", { name: "稍后重启" })).toHaveFocus();
      await userEvent.click(screen.getByRole("button", { name: "稍后重启" }));
      await userEvent.keyboard("{Escape}");
      expect(later).toHaveBeenCalledTimes(2);
      expect(restart).not.toHaveBeenCalled();
    },
  );
  it("桌面端立即重启传入对应客户端与配置版本", async () => {
    const restart = vi
      .spyOn(api, "restartClient")
      .mockResolvedValue({ restarted: true, message: "Claude 已重启" });
    const done = vi.fn();
    render(
      <ClientRestartDialog
        runtime={runtime("claude_desktop")}
        onLater={vi.fn()}
        onRestarted={done}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: "立即重启" }));
    expect(restart).toHaveBeenCalledExactlyOnceWith("claude_desktop", 456);
    await waitFor(() => expect(done).toHaveBeenCalledWith("Claude 已重启"));
  });
  it("CLI说明接续步骤，打开辅助终端只报告等待退出，不误报重启成功", async () => {
    const message = "已打开重启终端，请在原 Claude CLI 输入 /exit";
    const restart = vi
      .spyOn(api, "restartClient")
      .mockResolvedValue({ restarted: false, pending: true, message });
    const done = vi.fn();
    render(
      <ClientRestartDialog
        runtime={runtime("claude_cli")}
        onLater={vi.fn()}
        onRestarted={done}
      />,
    );
    expect(screen.getByText(/立即重启会打开一个接续终端/)).toBeVisible();
    expect(screen.getByText("claude --continue")).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "立即重启" }));
    expect(restart).toHaveBeenCalledExactlyOnceWith("claude_cli", 456);
    await waitFor(() => expect(done).toHaveBeenCalledWith(message));
  });
  it("Claude重启失败保留原因及稍后选项", async () => {
    vi.spyOn(api, "restartClient").mockRejectedValue({
      message: "客户端尚未退出；新配置已保存",
    });
    const done = vi.fn();
    render(
      <ClientRestartDialog
        runtime={runtime("claude_desktop")}
        onLater={vi.fn()}
        onRestarted={done}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: "立即重启" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("新配置已保存");
    expect(screen.getByRole("button", { name: "稍后重启" })).toBeEnabled();
    expect(done).not.toHaveBeenCalled();
  });
  it("多个CLI实例或工作目录不明时解释原因并保留手动步骤", () => {
    render(
      <ClientRestartDialog
        runtime={{
          ...runtime("claude_cli"),
          canRestartClient: false,
          restartReason: "检测到多个 CLI 实例",
        }}
        onLater={vi.fn()}
        onRestarted={vi.fn()}
      />,
    );
    expect(screen.getByText("检测到多个 CLI 实例")).toBeVisible();
    expect(screen.getByRole("button", { name: "立即重启" })).toBeDisabled();
    expect(screen.getByText("claude --continue")).toBeVisible();
  });
});
