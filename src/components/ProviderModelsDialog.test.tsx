import { useState } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  ProviderModelSelect,
  ProviderModelsDialog,
} from "./ProviderQuickSettings";
import { api } from "../lib/api";
import type { Provider, QuickModelInput } from "../types";

vi.mock("../lib/api", async (original) => ({
  ...(await original<typeof import("../lib/api")>()),
  desktopRuntime: true,
}));
const ids = [
  "gpt-6.1-sol",
  "codex-auto-review",
  "gpt-5.6-sol",
  "gpt-5.6-terra",
  "gpt-6-sol",
];
const models = ids.map((id) => ({
  id,
  enabled: true,
  contextWindow: 256000,
  reasoningEfforts: [],
}));
const imported: Provider = {
  id: "imported",
  family: "codex",
  name: "示例供应商",
  baseUrl: "https://gateway.test/v1",
  model: ids[0],
  authMode: "bearer",
  reasoningEffort: null,
  hasKey: true,
  keySuffix: "test",
  updatedAt: 1,
  codexOptions: {
    models: [models[0]],
    fastMode: false,
    contextWindow: null,
    autoCompactTokenLimit: null,
    modelsSyncedAt: null,
    balanceQuery: null,
  },
};
const commit = vi.fn();
beforeEach(() => {
  commit.mockReset();
  commit.mockResolvedValue(true);
  vi.spyOn(api, "discoverConnection").mockResolvedValue({
    models,
    protocol: "openai",
    authMode: "bearer",
    baseUrl: imported.baseUrl,
    syncedAt: 2,
  });
});
afterEach(() => vi.restoreAllMocks());
function Harness({
  initialProvider = imported,
  applied = false,
}: { initialProvider?: Provider; applied?: boolean } = {}) {
  const [provider, setProvider] = useState(initialProvider);
  const [open, setOpen] = useState(false);
  async function save(input: QuickModelInput) {
    const ok = await commit(input);
    if (ok)
      setProvider({
        ...provider,
        model: input.model,
        codexOptions: {
          ...provider.codexOptions!,
          models: input.models,
          modelsSyncedAt: input.syncedAt ?? null,
        },
      });
    return ok;
  }
  return (
    <>
      <ProviderModelSelect
        provider={provider}
        target="codex"
        displayedModel={provider.model}
        disabled={false}
        expanded={open}
        onExpand={(event) => {
          event.currentTarget.setAttribute("data-dialog-return", "");
          setOpen(true);
        }}
        onSave={save}
      />
      {open && (
        <ProviderModelsDialog
          provider={provider}
          target="codex"
          disabled={false}
          applied={applied}
          onClose={() => setOpen(false)}
          onSave={save}
        />
      )}
    </>
  );
}
describe("已保存配置自动同步后的模型计数", () => {
  it("原有一个模型、同步五个只形成草稿，取消后仍是一，确认后主列表立即变五", async () => {
    render(<Harness />);
    const user = userEvent.setup();
    const trigger = screen.getByRole("button", {
      name: "配置 示例供应商 的模型",
    });
    expect(trigger).toHaveTextContent("已启用 1 个");
    await user.click(trigger);
    await screen.findByText("已选择 5 / 5");
    expect(screen.getByText("当前已保存 1 个，保存后启用 5 个")).toBeVisible();
    expect(screen.queryByText("已启用 5 / 5")).toBeNull();
    expect(commit).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "取消" }));
    expect(trigger).toHaveTextContent("已启用 1 个");
    await waitFor(() => expect(trigger).toHaveFocus());
    expect(commit).not.toHaveBeenCalled();
    await user.click(trigger);
    await screen.findByText("已选择 5 / 5");
    await user.click(screen.getByRole("button", { name: "保存模型配置" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(trigger).toHaveTextContent("已启用 5 个");
    expect(commit).toHaveBeenCalledOnce();
    expect(
      commit.mock.calls[0][0].models.filter(
        (m: { enabled: boolean }) => m.enabled,
      ),
    ).toHaveLength(5);
    await user.click(trigger);
    expect(screen.getByText("当前已保存 5 个，保存后启用 5 个")).toBeVisible();
  });
  it("同步失败保留已保存数量，关闭弹窗不写入", async () => {
    vi.mocked(api.discoverConnection).mockRejectedValue(
      new Error("暂时无法连接"),
    );
    render(<Harness />);
    await userEvent.click(
      screen.getByRole("button", { name: "配置 示例供应商 的模型" }),
    );
    await screen.findByText(/暂时无法连接/);
    expect(screen.getByText("当前已保存 1 个，保存后启用 1 个")).toBeVisible();
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(commit).not.toHaveBeenCalled();
  });

  it("五个旧模型消失后仅显示最新四个；取消不改配置，确认应用只提交最新模型与可用默认值", async () => {
    const latest = [
      "DLM-v1",
      "deepseek-v4-flash-0731",
      "qwen-coder",
      "glm-new",
    ];
    vi.mocked(api.discoverConnection).mockResolvedValue({
      models: latest.map((id) => ({ ...models[0], id })),
      protocol: "openai",
      authMode: "bearer",
      baseUrl: imported.baseUrl,
      syncedAt: 3,
    });
    render(
      <Harness
        initialProvider={{
          ...imported,
          codexOptions: { ...imported.codexOptions!, models },
        }}
        applied
      />,
    );
    const user = userEvent.setup();
    const trigger = screen.getByRole("button", {
      name: "配置 示例供应商 的模型",
    });
    await user.click(trigger);
    await screen.findByText("已选择 4 / 4");
    for (const id of ids) {
      expect(screen.queryByLabelText(`启用 ${id}`)).toBeNull();
      expect(screen.queryByLabelText(`上下文长度 ${id}`)).toBeNull();
    }
    expect(screen.getByText("当前已保存 5 个，保存后启用 4 个")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "取消" }));
    expect(commit).not.toHaveBeenCalled();
    expect(trigger).toHaveTextContent("已启用 5 个");
    await user.click(trigger);
    await screen.findByText("已选择 4 / 4");
    await user.click(screen.getByRole("button", { name: "保存并应用" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(trigger).toHaveTextContent("已启用 4 个");
    const submitted = commit.mock.calls[0][0] as QuickModelInput;
    expect(submitted.models.map((m) => m.id)).toEqual(latest);
    expect(latest).toContain(submitted.model);
    expect(submitted.syncedAt).toBe(3);
  });
});

it("图片能力自动匹配，未知模型可手动配置；取消不写入，刷新保留草稿并确认提交", async () => {
  const custom = { ...models[0], id: "custom-vision" };
  vi.mocked(api.discoverConnection).mockResolvedValue({
    models: [models[0], custom],
    protocol: "openai",
    authMode: "bearer",
    baseUrl: imported.baseUrl,
    syncedAt: 2,
  });
  render(<Harness applied />);
  const user = userEvent.setup();
  const open = async () => {
    await user.click(
      screen.getByRole("button", { name: "配置 示例供应商 的模型" }),
    );
    await screen.findByText("已选择 2 / 2");
  };
  await open();
  expect(screen.getByLabelText(`图片输入 ${ids[0]}`)).toBeChecked();
  expect(screen.getByLabelText("图片输入 custom-vision")).not.toBeChecked();
  await user.click(screen.getByLabelText("图片输入 custom-vision"));
  await user.click(screen.getByRole("button", { name: "取消" }));
  expect(commit).not.toHaveBeenCalled();
  await open();
  expect(screen.getByLabelText("图片输入 custom-vision")).not.toBeChecked();
  await user.click(screen.getByLabelText("图片输入 custom-vision"));
  await user.click(screen.getByRole("button", { name: "刷新模型列表" }));
  await waitFor(() =>
    expect(vi.mocked(api.discoverConnection)).toHaveBeenCalledTimes(3),
  );
  expect(screen.getByLabelText("图片输入 custom-vision")).toBeChecked();
  await user.click(
    screen.getByRole("button", { name: "恢复自动能力 custom-vision" }),
  );
  expect(screen.getByLabelText("图片输入 custom-vision")).not.toBeChecked();
  await user.click(screen.getByLabelText("图片输入 custom-vision"));
  await user.click(screen.getByRole("button", { name: "保存并应用" }));
  await waitFor(() => expect(commit).toHaveBeenCalledOnce());
  expect(
    (commit.mock.calls[0][0] as QuickModelInput).models.find(
      (m) => m.id === "custom-vision",
    )?.capabilityOverrides?.imageInput,
  ).toBe(true);
});
