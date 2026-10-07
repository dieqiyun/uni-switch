import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { AppUpdateEntry } from "./AppUpdateEntry";
import type { AppUpdateState } from "../lib/useAppUpdate";
import { APP_VERSION } from "../lib/appVersion";
const state = (
  available: boolean,
  checking = false,
  error: Error | null = null,
) =>
  ({
    check: {
      data: { available, latestVersion: "99.0.0" },
      isFetching: checking,
      error,
    },
  }) as AppUpdateState;
describe("左下角明确更新提醒", () => {
  it("显示目标版本和立即更新，点击与键盘操作均打开更新入口", async () => {
    const open = vi.fn();
    render(
      <AppUpdateEntry state={state(true)} expanded={false} onClick={open} />,
    );
    expect(screen.getByRole("status")).toHaveTextContent("发现新版本 v99.0.0");
    expect(screen.getByText("立即更新")).toBeVisible();
    expect(screen.getByText(`v${APP_VERSION}`)).toBeVisible();
    const button = screen.getByRole("button", { name: /发现新版本 v99.0.0/ });
    await userEvent.click(button);
    await userEvent.keyboard("{Enter}");
    expect(open).toHaveBeenCalledTimes(2);
    expect(button).toHaveAttribute("aria-haspopup", "dialog");
  });
  it("再次检查和网络失败保留已有提醒，成功确认没有新版后才消除", () => {
    const props = { expanded: false, onClick: vi.fn() };
    const { rerender } = render(
      <AppUpdateEntry {...props} state={state(true)} />,
    );
    rerender(<AppUpdateEntry {...props} state={state(true, true)} />);
    expect(screen.getByText("立即更新")).toBeVisible();
    rerender(
      <AppUpdateEntry
        {...props}
        state={state(true, false, new Error("网络失败"))}
      />,
    );
    expect(screen.getByRole("status")).toHaveTextContent("v99.0.0");
    rerender(<AppUpdateEntry {...props} state={state(false)} />);
    expect(screen.queryByRole("status")).toBeNull();
    expect(screen.queryByText("立即更新")).toBeNull();
    expect(screen.getByText("检查更新")).toBeVisible();
  });
});
