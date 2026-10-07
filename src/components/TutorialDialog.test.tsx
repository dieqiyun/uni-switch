import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { TutorialDialog } from "./TutorialDialog";
import { api } from "../lib/api";
import tutorial from "../content/tutorial.json";
vi.mock("../lib/api", async (original) => ({
  ...(await original<typeof import("../lib/api")>()),
  desktopRuntime: true,
}));
afterEach(() => vi.restoreAllMocks());
describe("离线使用教程", () => {
  it("八个分类切换可读，常见问题说明冲突不能强制覆盖", async () => {
    render(<TutorialDialog onClose={vi.fn()} />);
    const nav = screen.getByRole("navigation", { name: "教程目录" });
    for (const topic of tutorial.topics) {
      const button = within(nav).getByRole("button", { name: topic.title });
      await userEvent.click(button);
      expect(button).toHaveAttribute("aria-current", "page");
      expect(
        within(screen.getByRole("article")).getByRole("heading", {
          name: topic.title,
        }),
      ).toBeVisible();
    }
    expect(screen.getByText(/存在冲突时不会强制覆盖文件/)).toBeVisible();
  });
  it("打开固定 GitHub 教程，不发送供应商资料，失败给出可复制地址", async () => {
    const open = vi
      .spyOn(api, "openProjectPage")
      .mockRejectedValue(new Error("浏览器启动失败"));
    render(<TutorialDialog onClose={vi.fn()} initialTopic="troubleshooting" />);
    expect(screen.getByRole("button", { name: "常见问题" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    const link = screen.getByRole("link", { name: "GitHub 完整教程" });
    expect(link).toHaveAttribute(
      "href",
      "https://github.com/dieqiyun/uni-switch/blob/main/docs/tutorial.md",
    );
    await userEvent.click(link);
    expect(open).toHaveBeenCalledExactlyOnceWith("tutorial");
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "浏览器启动失败",
    );
    expect(screen.getByRole("alert")).toHaveTextContent("docs/tutorial.md");
  });
});
