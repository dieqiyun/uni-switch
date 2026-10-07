import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ServicePromotion, SERVICE_WEBSITE } from "./ServicePromotion";
import { api } from "../lib/api";

const mode = vi.hoisted(() => ({ desktop: true }));
vi.mock("../lib/api", async (original) => ({
  ...(await original<typeof import("../lib/api")>()),
  get desktopRuntime() {
    return mode.desktop;
  },
}));
beforeEach(() => {
  mode.desktop = true;
});
afterEach(() => {
  vi.restoreAllMocks();
});

describe("底部官网宣传入口", () => {
  it("官网内容和链接可辨识，桌面用键盘打开固定官网，不调用供应商操作", async () => {
    const open = vi.spyOn(api, "openServiceWebsite").mockResolvedValue();
    const apply = vi.spyOn(api, "apply");
    render(<ServicePromotion />);
    const link = screen.getByRole("link", {
      name: "访问蝶祈云 API 官网（在浏览器中打开）",
    });
    expect(link).toHaveAttribute("href", SERVICE_WEBSITE);
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
    expect(screen.getByText("GPT 真官 Key · 机构合作定制 · 优质渠道聚合 · 支持开具发票")).toBeVisible();
    link.focus();
    await userEvent.keyboard("{Enter}");
    expect(open).toHaveBeenCalledExactlyOnceWith();
    expect(apply).not.toHaveBeenCalled();
    expect(link).toHaveFocus();
  });
  it("打开中防止重复调用，失败显示官网地址，允许重试", async () => {
    let reject!: (value: unknown) => void;
    const open = vi.spyOn(api, "openServiceWebsite").mockImplementationOnce(
      () =>
        new Promise((_resolve, fail) => {
          reject = fail;
        }),
    );
    render(<ServicePromotion />);
    const link = screen.getByRole("link");
    await userEvent.click(link);
    expect(link).toHaveAttribute("aria-disabled", "true");
    expect(link).toHaveAttribute("aria-busy", "true");
    expect(screen.getByText("打开中…")).toBeVisible();
    await userEvent.click(link);
    expect(open).toHaveBeenCalledOnce();
    reject({ message: "未能打开浏览器，请手动访问官网" });
    expect(await screen.findByRole("alert")).toHaveTextContent(SERVICE_WEBSITE);
    await waitFor(() => expect(link).not.toHaveAttribute("aria-disabled"));
    open.mockResolvedValueOnce();
    await userEvent.click(link);
    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
    expect(open).toHaveBeenCalledTimes(2);
  });
  it("浏览器预览提供原生新窗口链接", async () => {
    mode.desktop = false;
    const open = vi.spyOn(api, "openServiceWebsite");
    render(<ServicePromotion />);
    const link = screen.getByRole("link");
    expect(link).toHaveAttribute("href", "https://www.dieqiyun.top/");
    expect(link).toHaveAttribute("target", "_blank");
    await userEvent.click(link);
    expect(open).not.toHaveBeenCalled();
  });
});
