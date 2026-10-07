import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { beforeEach, expect, it, vi } from "vitest";
import { ProviderName } from "./ProviderName";
import type { Provider } from "../types";

const provider: Provider = {
  id: "rename",
  family: "codex",
  name: "原名称",
  baseUrl: "https://gateway.test/v1",
  model: "gpt-test",
  authMode: "bearer",
  reasoningEffort: null,
  hasKey: true,
  keySuffix: "test",
  updatedAt: 1,
};
const save = vi.fn();
beforeEach(() => {
  vi.clearAllMocks();
  save.mockResolvedValue(true);
});
function Row() {
  const [editing, setEditing] = useState(false);
  return (
    <ProviderName
      provider={provider}
      displayName={provider.name}
      disabled={false}
      editing={editing}
      onEditing={setEditing}
      onSave={save}
    />
  );
}
it("名称直接在行内编辑，Enter保存；Escape与取消不写入", async () => {
  render(<Row />);
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "修改名称 原名称" }));
  const input = screen.getByLabelText("供应商名称 · 原名称");
  expect(input).toHaveFocus();
  await user.clear(input);
  await user.type(input, "新名称{Enter}");
  expect(save).toHaveBeenCalledWith(provider, "新名称");
  await waitFor(() =>
    expect(screen.queryByLabelText("供应商名称 · 原名称")).toBeNull(),
  );
  save.mockClear();
  await user.click(screen.getByRole("button", { name: "修改名称 原名称" }));
  await user.clear(screen.getByLabelText("供应商名称 · 原名称"));
  await user.keyboard("丢弃{Escape}");
  expect(save).not.toHaveBeenCalled();
  expect(screen.queryByRole("dialog")).toBeNull();
});
it("空名称被阻止，保存失败后仍保留名称草稿", async () => {
  save.mockResolvedValue(false);
  render(<Row />);
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "修改名称 原名称" }));
  const input = screen.getByLabelText("供应商名称 · 原名称");
  await user.clear(input);
  await user.keyboard("{Enter}");
  expect(save).not.toHaveBeenCalled();
  expect(input).toHaveAttribute("aria-invalid", "true");
  await user.type(input, "保留草稿{Enter}");
  expect(save).toHaveBeenCalledOnce();
  expect(input).toHaveValue("保留草稿");
});
