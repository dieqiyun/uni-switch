import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { WindowChrome } from "./WindowChrome";

const native = vi.hoisted(() => ({
  desktop: true,
  maximized: false,
  resized: null as null | (() => void),
  stop: vi.fn(),
  minimize: vi.fn(),
  toggleMaximize: vi.fn(),
  close: vi.fn(),
  isMaximized: vi.fn(),
  onResized: vi.fn(),
  getWindow: vi.fn(),
}));
vi.mock("../lib/api", () => ({
  get desktopRuntime() {
    return native.desktop;
  },
}));
vi.mock("@tauri-apps/api/window", () => ({
  getCurrentWindow: native.getWindow,
}));

beforeEach(() => {
  vi.clearAllMocks();
  native.desktop = true;
  native.maximized = false;
  native.resized = null;
  native.getWindow.mockReturnValue(native);
  native.isMaximized.mockImplementation(async () => native.maximized);
  native.onResized.mockImplementation(async (listener: () => void) => {
    native.resized = listener;
    return native.stop;
  });
  native.minimize.mockResolvedValue(undefined);
  native.close.mockResolvedValue(undefined);
  native.toggleMaximize.mockImplementation(async () => {
    native.maximized = !native.maximized;
    native.resized?.();
  });
});

describe("自定义窗口操作", () => {
  it("浏览器预览不创建原生窗口操作", () => {
    native.desktop = false;
    render(<WindowChrome />);
    expect(screen.queryByRole("group", { name: "窗口操作" })).toBeNull();
    expect(native.getWindow).not.toHaveBeenCalled();
  });

  it("按钮和键盘分别调用最小化、最大化、还原和原生关闭", async () => {
    const user = userEvent.setup();
    render(<WindowChrome />);
    await user.click(screen.getByRole("button", { name: "最小化窗口" }));
    expect(native.minimize).toHaveBeenCalledOnce();
    await user.click(screen.getByRole("button", { name: "最大化窗口" }));
    await user.click(await screen.findByRole("button", { name: "还原窗口" }));
    expect(native.toggleMaximize).toHaveBeenCalledTimes(2);
    const close = screen.getByRole("button", { name: "关闭窗口并留在托盘" });
    close.focus();
    await user.keyboard("{Enter}");
    expect(native.close).toHaveBeenCalledOnce();
  });

  it("系统最大化事件更新还原按钮，并在卸载后移除监听", async () => {
    const { unmount } = render(<WindowChrome />);
    await waitFor(() => expect(native.resized).toBeTypeOf("function"));
    native.maximized = true;
    await act(async () => native.resized?.());
    expect(screen.getByRole("button", { name: "还原窗口" })).toBeVisible();
    unmount();
    expect(native.stop).toHaveBeenCalledOnce();
  });

  it("处理过程中阻止连点；失败显示提示后可以重试", async () => {
    let reject: (error: Error) => void = () => {};
    native.minimize.mockImplementationOnce(
      () =>
        new Promise((_, fail) => {
          reject = fail;
        }),
    );
    const user = userEvent.setup();
    render(<WindowChrome />);
    await user.dblClick(screen.getByRole("button", { name: "最小化窗口" }));
    expect(native.minimize).toHaveBeenCalledOnce();
    expect(screen.getByRole("button", { name: "最小化窗口" })).toBeDisabled();
    await act(async () => reject(new Error("native failure")));
    expect(screen.getByRole("alert")).toHaveTextContent(
      "窗口操作未完成，请重试。",
    );
    await user.click(screen.getByRole("button", { name: "最小化窗口" }));
    expect(native.minimize).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("注册监听期间卸载也会清理监听", async () => {
    let finish: (stop: () => void) => void = () => {};
    native.onResized.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const { unmount } = render(<WindowChrome />);
    unmount();
    await act(async () => finish(native.stop));
    expect(native.stop).toHaveBeenCalledOnce();
  });
});
