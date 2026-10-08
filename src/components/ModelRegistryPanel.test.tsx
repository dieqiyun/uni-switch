import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import registry from "../content/model-capabilities.json";
import { api } from "../lib/api";
import type { ModelRegistry, ModelRegistryStatus } from "../types";
import { ModelRegistryPanel } from "./ModelRegistryPanel";

const status: ModelRegistryStatus = {
  registry: registry as ModelRegistry,
  checkedAt: null,
  updated: false,
  updateAvailable: false,
  message: "使用内置资料",
};
afterEach(() => vi.restoreAllMocks());
describe("独立模型资料更新", () => {
  it("没有签名源时明确说明未配置，不声称已是最新", async () => {
    vi.spyOn(api, "modelRegistry").mockResolvedValue(status);
    const update = vi.spyOn(api, "updateModelRegistry");
    render(<ModelRegistryPanel />);
    await screen.findByText("使用内置资料");
    expect(screen.getByRole("button", { name: "更新模型资料" })).toBeDisabled();
    expect(screen.getByText(/尚未配置签名更新源/)).toBeVisible();
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
      .mockRejectedValue(new Error("签名不匹配，已保留原资料"));
    render(<ModelRegistryPanel />);
    await screen.findByText("使用内置资料");
    await userEvent.click(screen.getByRole("button", { name: "更新模型资料" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "签名不匹配，已保留原资料",
    );
    expect(screen.getByRole("button", { name: "更新模型资料" })).toBeEnabled();
    expect(update).toHaveBeenCalledOnce();
  });
});
