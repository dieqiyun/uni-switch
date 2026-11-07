import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "../lib/api";
import type { ModelRegistryStatus } from "../types";
import { ModelRegistryPanel } from "./ModelRegistryPanel";

const status: ModelRegistryStatus = {
  registry: {
    schemaVersion: 1,
    version: 0,
    verifiedAt: "尚无缓存",
    entries: [],
  },
  checkedAt: null,
  updated: false,
  updateAvailable: false,
  message: "没有可用目录缓存",
};
afterEach(() => vi.restoreAllMocks());
describe("独立模型资料更新", () => {
  it("浏览器预览不下载目录，不声称已是最新", async () => {
    vi.spyOn(api, "modelRegistry").mockResolvedValue(status);
    const update = vi.spyOn(api, "updateModelRegistry");
    render(<ModelRegistryPanel />);
    await screen.findByText("没有可用目录缓存");
    expect(screen.getByRole("button", { name: "更新模型资料" })).toBeDisabled();
    expect(screen.getByText(/当前环境不更新目录/)).toBeVisible();
    expect(screen.queryByText("模型资料已是最新")).toBeNull();
    expect(update).not.toHaveBeenCalled();
  });
  it("更新错误保留离线资料并允许重试", async () => {
    vi.spyOn(api, "modelRegistry").mockResolvedValue({
      ...status,
      updateAvailable: true,
    });
    const update = vi
      .spyOn(api, "updateModelRegistry")
      .mockRejectedValue(new Error("目录下载失败，已保留缓存"));
    render(<ModelRegistryPanel />);
    await screen.findByText("没有可用目录缓存");
    await userEvent.click(screen.getByRole("button", { name: "更新模型资料" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "目录下载失败，已保留缓存",
    );
    expect(screen.getByRole("button", { name: "更新模型资料" })).toBeEnabled();
    expect(update).toHaveBeenCalledOnce();
  });
});
