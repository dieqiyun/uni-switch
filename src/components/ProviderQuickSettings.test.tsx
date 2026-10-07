import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  ProviderModelSelect,
  ProviderModelsDialog,
} from "./ProviderQuickSettings";
import type { Provider } from "../types";

const provider: Provider = {
  id: "quick",
  family: "codex",
  name: "快捷供应商",
  baseUrl: "https://gateway.test/v1",
  authMode: "bearer",
  model: "gpt-one",
  reasoningEffort: null,
  hasKey: true,
  keySuffix: "test",
  updatedAt: 123,
  codexOptions: {
    models: [
      {
        id: "gpt-one",
        enabled: true,
        contextWindow: 256000,
        reasoningEfforts: [],
      },
      {
        id: "gpt-two",
        enabled: false,
        contextWindow: 512000,
        reasoningEfforts: [],
      },
    ],
    modelsSyncedAt: 123,
    fastMode: false,
    contextWindow: null,
    autoCompactTokenLimit: null,
    balanceQuery: null,
  },
};
const save = vi.fn(),
  close = vi.fn();
beforeEach(() => {
  vi.clearAllMocks();
  save.mockResolvedValue(true);
});
function select() {
  return render(
    <ProviderModelSelect
      provider={provider}
      target="codex"
      displayedModel="gpt-one"
      disabled={false}
      expanded={false}
      onExpand={close}
      onSave={save}
    />,
  );
}
function panel() {
  return render(
    <ProviderModelsDialog
      provider={provider}
      target="codex"
      disabled={false}
      applied
      onClose={close}
      onSave={save}
    />,
  );
}
describe("供应商行内配置", () => {
  it("模型只保留一个平铺入口，键盘打开配置不直接写入", async () => {
    select();
    const control = screen.getByRole("button", {
      name: "配置 快捷供应商 的模型",
    });
    expect(control).toHaveTextContent("gpt-one");
    expect(control).toHaveAttribute("aria-haspopup", "dialog");
    expect(screen.queryByRole("combobox")).toBeNull();
    control.focus();
    await userEvent.keyboard("{Enter}");
    expect(close).toHaveBeenCalledOnce();
    expect(save).not.toHaveBeenCalled();
  });
  it("上下文输入期间不写入，Enter提交一次，Escape取消且非法值不提交", async () => {
    select();
    const user = userEvent.setup();
    const field = screen.getByLabelText("上下文长度 · 快捷供应商 · gpt-one");
    await user.clear(field);
    await user.type(field, "128");
    expect(save).not.toHaveBeenCalled();
    await user.keyboard("{Enter}");
    expect(save).toHaveBeenCalledOnce();
    expect(save.mock.calls[0][0].models[0].contextWindow).toBe(128000);
    save.mockClear();
    await user.clear(field);
    await user.type(field, "64");
    await user.keyboard("{Escape}");
    expect(save).not.toHaveBeenCalled();
    expect(field).toHaveValue("256");
    await user.clear(field);
    await user.type(field, "oops");
    await user.tab();
    expect(save).not.toHaveBeenCalled();
    expect(field).toHaveAttribute("aria-invalid", "true");
  });
  it("上下文写入失败恢复原值，列表不显示手动修复按钮", async () => {
    save.mockResolvedValueOnce(false);
    select();
    const user = userEvent.setup();
    const field = screen.getByLabelText("上下文长度 · 快捷供应商 · gpt-one");
    await user.clear(field);
    await user.type(field, "128{Enter}");
    await waitFor(() => expect(field).toHaveValue("256"));
    expect(screen.queryByRole("button", { name: /修复.*思考强度/ })).toBeNull();
    expect(save).toHaveBeenCalledOnce();
  });
  it("批量勾选和上下文仅留在弹窗草稿，取消不提交", async () => {
    panel();
    const user = userEvent.setup();
    expect(screen.queryByRole("button", { name: /修复.*思考强度/ })).toBeNull();
    expect(screen.getByText(/每次写入 Codex 配置时自动检查/)).toBeVisible();
    expect(screen.getByRole("region", { name: "本次选择" })).toHaveFocus();
    await user.click(screen.getByLabelText("启用 gpt-two"));
    const field = screen.getByLabelText("上下文长度 gpt-two");
    await user.clear(field);
    await user.type(field, "128");
    expect(screen.getByText("已选择 2 / 2")).toBeVisible();
    expect(screen.getByText("当前已保存 1 个，保存后启用 2 个")).toBeVisible();
    expect(save).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "取消" }));
    expect(close).toHaveBeenCalledOnce();
    expect(save).not.toHaveBeenCalled();
  });
  it("批量保存只提交一次，失败保留草稿且允许重试", async () => {
    save.mockResolvedValueOnce(false);
    panel();
    const user = userEvent.setup();
    await user.click(
      screen.getByRole("button", { name: "将 gpt-two 设为默认模型" }),
    );
    await user.click(screen.getByRole("button", { name: "保存并应用" }));
    expect(save).toHaveBeenCalledOnce();
    expect(close).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toHaveTextContent("未能保存");
    await user.click(screen.getByRole("button", { name: "保存并应用" }));
    expect(save.mock.calls[1][0].model).toBe("gpt-two");
    expect(close).toHaveBeenCalledOnce();
  });
  it("全部取消时阻止保存，不悄悄重新启用模型", async () => {
    panel();
    const user = userEvent.setup();
    await user.click(screen.getByLabelText("启用 gpt-one"));
    await user.click(screen.getByRole("button", { name: "保存并应用" }));
    expect(save).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toHaveTextContent("请至少启用");
  });
});
