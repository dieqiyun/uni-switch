import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CodexRestartDialog } from "./CodexRestartDialog";
import { api } from "../lib/api";
const runtime = {
  target: "codex" as const,
  clientRunning: true,
  restartRequired: true,
  bridgeRequired: false,
  bridgeHealthy: true,
  desktopRunning: true,
  desktopRestartRequired: true,
  canRestartDesktop: true,
  configurationRevision: 123,
};
afterEach(() => vi.restoreAllMocks());
describe("Codex 重启选择", () => {
  it("默认聚焦稍后重启，稍后和Escape不执行重启", async () => {
    const later = vi.fn(),
      restarted = vi.fn(),
      restart = vi.spyOn(api, "restartCodex");
    render(
      <CodexRestartDialog
        runtime={runtime}
        onLater={later}
        onRestarted={restarted}
      />,
    );
    expect(screen.getByRole("button", { name: "稍后重启" })).toHaveFocus();
    expect(screen.getByText("切换后请新开对话")).toBeVisible();
    expect(screen.getByText(/即使重启 Codex 或恢复旧会话/)).toBeVisible();
    expect(screen.getByText(/供应商使用记录反映实际请求/)).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "稍后重启" }));
    expect(later).toHaveBeenCalledOnce();
    await userEvent.keyboard("{Escape}");
    expect(later).toHaveBeenCalledTimes(2);
    expect(restart).not.toHaveBeenCalled();
  });
  it("立即重启使用当前配置版本，等待期间禁止重复点击，成功回报", async () => {
    let resolve!: (value: { restarted: boolean; message: string }) => void;
    const restart = vi.spyOn(api, "restartCodex").mockImplementation(
      () =>
        new Promise((r) => {
          resolve = r;
        }),
    );
    const later = vi.fn(),
      done = vi.fn();
    render(
      <CodexRestartDialog
        runtime={runtime}
        onLater={later}
        onRestarted={done}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: "立即重启" }));
    expect(restart).toHaveBeenCalledExactlyOnceWith(123);
    expect(screen.getByRole("button", { name: "正在重启…" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "稍后重启" })).toBeDisabled();
    await userEvent.keyboard("{Escape}");
    expect(later).not.toHaveBeenCalled();
    resolve({ restarted: true, message: "Codex 已重启" });
    await waitFor(() => expect(done).toHaveBeenCalledWith("Codex 已重启"));
  });
  it("重启失败保留弹窗及稍后入口，可再次尝试", async () => {
    vi.spyOn(api, "restartCodex")
      .mockRejectedValueOnce({ message: "Codex 尚未退出，新配置已保存" })
      .mockResolvedValue({ restarted: true, message: "Codex 已重启" });
    const done = vi.fn();
    render(
      <CodexRestartDialog
        runtime={runtime}
        onLater={vi.fn()}
        onRestarted={done}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: "立即重启" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("新配置已保存");
    expect(screen.getByRole("button", { name: "稍后重启" })).toBeEnabled();
    expect(done).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "立即重启" }));
    await waitFor(() => expect(done).toHaveBeenCalledOnce());
  });
  it("无法自动重启时展示原因，不执行未知实例操作", async () => {
    const restart = vi.spyOn(api, "restartCodex");
    render(
      <CodexRestartDialog
        runtime={{
          ...runtime,
          canRestartDesktop: false,
          restartReason: "检测到多个 Codex 实例",
        }}
        onLater={vi.fn()}
        onRestarted={vi.fn()}
      />,
    );
    expect(screen.getByText("检测到多个 Codex 实例")).toBeVisible();
    expect(screen.getByRole("button", { name: "立即重启" })).toBeDisabled();
    expect(restart).not.toHaveBeenCalled();
  });
});
