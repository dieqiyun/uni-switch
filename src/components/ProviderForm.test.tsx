import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ProviderForm } from "./ProviderForm";
import { api } from "../lib/api";
import type { ModelSyncResult, Provider, Target } from "../types";
vi.mock("../lib/api", async (original) => ({
  ...(await original<typeof import("../lib/api")>()),
  desktopRuntime: true,
}));
const models = (
  protocol: "openai" | "anthropic" = "openai",
): ModelSyncResult => ({
  syncedAt: 123,
  protocol,
  authMode: protocol === "anthropic" ? "x-api-key" : "bearer",
  models: (protocol === "openai"
    ? ["gpt-4.1", "gpt-5.4", "text-embedding-3-large"]
    : ["claude-opus-4-6", "claude-sonnet-4-6"]
  ).map((id) => ({
    id,
    enabled: true,
    contextWindow: null,
    reasoningEfforts: [],
  })),
});
const saved: Provider = {
  id: "shared",
  family: "codex",
  name: "共享 API",
  baseUrl: "https://example.test/v1",
  model: "gpt-5.4",
  authMode: "bearer",
  hasKey: true,
  keySuffix: "1234",
  updatedAt: 1,
  reasoningEffort: null,
};
function mount(
  provider?: Provider,
  target: Target = "codex",
  applyOnSave = true,
) {
  const commit = vi.fn().mockResolvedValue(saved),
    close = vi.fn(),
    complete = vi.fn();
  render(
    <ProviderForm
      family={target === "codex" ? "codex" : "claude"}
      target={target}
      provider={provider}
      applyOnSave={applyOnSave}
      onCommit={commit}
      onComplete={complete}
      onClose={close}
    />,
  );
  return { commit, close, complete };
}
beforeEach(() => {
  vi.spyOn(api, "discoverConnection").mockResolvedValue(models());
  vi.spyOn(api, "balance").mockRejectedValue(new Error("余额暂不可用"));
});
afterEach(() => vi.restoreAllMocks());
const fields = async () => {
  const user = userEvent.setup();
  await user.type(
    screen.getByLabelText(/API 地址/),
    "https://api.example.test/v1",
  );
  await user.type(screen.getByLabelText(/^API Key/), "fake-test-key");
  return user;
};
describe("精简供应商流程", () => {
  it("模型列表在添加和编辑时直接展示，没有折叠操作", async () => {
    mount(saved);
    const heading = screen.getByText("模型设置");
    expect(heading.closest("details")).toBeNull();
    expect(screen.getByRole("heading", { name: "模型设置" })).toBeVisible();
    expect(await screen.findByLabelText("启用 gpt-5.4")).toBeVisible();
    await userEvent.click(heading);
    expect(screen.getByLabelText("启用 gpt-5.4")).toBeVisible();
    expect(screen.getByLabelText("上下文长度 gpt-5.4")).toBeVisible();
  });
  it("完整请求地址和Bearer密钥直接按Enter，只使用整理后的连接", async () => {
    const { commit } = mount();
    const user = userEvent.setup();
    await user.type(
      screen.getByLabelText(/API 地址/),
      "https://api.example.test/team/v1/chat/completions",
    );
    await user.click(screen.getByLabelText(/^API Key/));
    await user.paste(' "Bearer fake-test-key" ');
    await user.keyboard("{Enter}");
    await waitFor(() => expect(commit).toHaveBeenCalledOnce());
    expect(api.discoverConnection).toHaveBeenCalledWith(
      expect.objectContaining({
        baseUrl: "https://api.example.test/team/v1",
        apiKey: "fake-test-key",
      }),
      null,
      null,
    );
    expect(commit).toHaveBeenCalledWith(
      expect.objectContaining({
        baseUrl: "https://api.example.test/team/v1",
        apiKey: "fake-test-key",
      }),
      true,
    );
  });
  it("未启用模型一步启用并设为默认，其他选择和上下文保留", async () => {
    const { commit } = mount();
    const user = await fields();
    const first = await screen.findByRole("checkbox", { name: "启用 gpt-4.1" });
    expect(first).toBeChecked();
    expect(screen.getByText("已启用 2 / 2")).toBeVisible();
    await user.click(first);
    expect(first).not.toBeChecked();
    await user.click(
      await screen.findByRole("button", { name: "将 gpt-4.1 设为默认模型" }),
    );
    expect(
      screen.getByRole("checkbox", { name: "启用 gpt-4.1" }),
    ).toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: "启用 gpt-5.4" }),
    ).toBeChecked();
    await user.click(screen.getByRole("button", { name: "添加并使用" }));
    expect(commit).toHaveBeenCalledWith(
      expect.objectContaining({
        model: "gpt-4.1",
        codexOptions: expect.objectContaining({
          models: expect.arrayContaining([
            expect.objectContaining({
              id: "gpt-4.1",
              enabled: true,
              contextWindow: 256000,
            }),
            expect.objectContaining({ id: "gpt-5.4", enabled: true }),
          ]),
        }),
      }),
      true,
    );
  });
  it("大量模型可搜索未展示项并只看已启用，过滤不丢失选择", async () => {
    vi.spyOn(api, "discoverConnection").mockResolvedValue({
      ...models(),
      models: Array.from({ length: 85 }, (_, i) => ({
        id: `gpt-${i}`,
        enabled: false,
        contextWindow: null,
        reasoningEfforts: [],
      })),
    });
    const { commit } = mount();
    const user = await fields();
    await screen.findByRole("button", { name: "显示更多模型" });
    expect(screen.getAllByRole("checkbox", { name: /^启用 / })).toHaveLength(
      40,
    );
    await user.type(screen.getByRole("textbox", { name: "搜索模型" }), "gpt-0");
    await user.click(
      screen.getByRole("button", { name: "将 gpt-0 设为默认模型" }),
    );
    await user.click(screen.getByRole("button", { name: /只看已启用/ }));
    expect(screen.getAllByRole("checkbox", { name: /^启用 / })).toHaveLength(
      40,
    );
    await user.click(screen.getByRole("button", { name: "添加并使用" }));
    expect(commit).toHaveBeenCalledWith(
      expect.objectContaining({
        model: "gpt-0",
        codexOptions: expect.objectContaining({ models: expect.any(Array) }),
      }),
      true,
    );
    expect(
      commit.mock.calls[0][0].codexOptions.models.filter(
        (m: { enabled: boolean }) => m.enabled,
      ),
    ).toHaveLength(85);
  });
  it("点击弹窗外侧保留表单，明确取消仍不写入", async () => {
    const { commit, close } = mount();
    const user = await fields();
    await user.click(document.querySelector(".modal-overlay")!);
    expect(close).not.toHaveBeenCalled();
    expect(screen.getByLabelText(/^API Key/)).toHaveValue("fake-test-key");
    await user.click(screen.getByRole("button", { name: "取消" }));
    expect(close).toHaveBeenCalledOnce();
    expect(commit).not.toHaveBeenCalled();
  });
  it("自动发现带v1的模型接口后保存对应接入路径，无需修改输入", async () => {
    vi.spyOn(api, "discoverConnection").mockResolvedValue({
      ...models(),
      baseUrl: "https://api.example.test/v1",
    });
    const { commit } = mount();
    const user = await fields();
    await user.clear(screen.getByLabelText(/API 地址/));
    await user.type(
      screen.getByLabelText(/API 地址/),
      "https://api.example.test",
    );
    await user.click(screen.getByRole("button", { name: "添加并使用" }));
    expect(commit).toHaveBeenCalledWith(
      expect.objectContaining({ baseUrl: "https://api.example.test/v1" }),
      true,
    );
  });
  it("高级设置固定展示且保留默认值，只填两个字段即可取得模型并使用", async () => {
    const { commit, complete, close } = mount();
    const user = await fields();
    expect(screen.getByLabelText("供应商名称")).toBeVisible();
    expect(screen.getByRole("group", { name: "API 协议" })).toBeVisible();
    expect(screen.getByRole("group", { name: "认证方式" })).toBeVisible();
    expect(screen.getByRole("group", { name: "Fast 模式" })).toBeVisible();
    expect(document.querySelector(".form-advanced")).toHaveProperty(
      "tagName",
      "SECTION",
    );
    expect(
      screen.queryByRole("button", { name: "保存配置" }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "添加并使用" }));
    await waitFor(() => expect(commit).toHaveBeenCalledOnce());
    expect(commit).toHaveBeenCalledWith(
      expect.objectContaining({
        name: "example.test",
        model: "gpt-5.4",
        authMode: "bearer",
        codexOptions: expect.objectContaining({
          upstreamProtocol: "openai",
          protocolPreference: null,
          authPreference: null,
          models: expect.arrayContaining([
            expect.objectContaining({ id: "gpt-4.1", enabled: true }),
            expect.objectContaining({ id: "gpt-5.4", enabled: true }),
            expect.objectContaining({
              id: "text-embedding-3-large",
              enabled: false,
            }),
          ]),
        }),
      }),
      true,
    );
    expect(complete).toHaveBeenCalledWith(true);
    expect(close).toHaveBeenCalledOnce();
  });
  it("按 Enter 与点击主按钮一致，等待已开始的探测而不重复请求", async () => {
    let resolve!: (result: ModelSyncResult) => void;
    const probe = vi.spyOn(api, "discoverConnection").mockImplementation(
      () =>
        new Promise((r) => {
          resolve = r;
        }),
    );
    const { commit } = mount();
    const user = await fields();
    await waitFor(() => expect(probe).toHaveBeenCalledOnce());
    await user.keyboard("{Enter}");
    expect(commit).not.toHaveBeenCalled();
    resolve(models());
    await waitFor(() => expect(commit).toHaveBeenCalledOnce());
    expect(probe).toHaveBeenCalledOnce();
  });
  it("模型、上下文和 Fast 先留在草稿，自动修复不绕过取消", async () => {
    const write = vi.spyOn(api, "writeModels"),
      repair = vi.spyOn(api, "repairReasoningLevels");
    const { commit, close } = mount(saved);
    const user = userEvent.setup();
    await screen.findByText("默认使用 gpt-5.4");
    await screen.findByRole("checkbox", { name: "启用 gpt-4.1" });
    await user.click(screen.getByRole("checkbox", { name: "启用 gpt-4.1" }));
    await user.clear(screen.getByLabelText("上下文长度 gpt-5.4"));
    await user.type(screen.getByLabelText("上下文长度 gpt-5.4"), "512");
    await user.click(screen.getByRole("radio", { name: "开启" }));
    expect(screen.queryByRole("button", { name: /修复.*思考强度/ })).toBeNull();
    expect(screen.getByText(/每次写入 Codex 配置时自动检查/)).toBeVisible();
    await user.click(screen.getByRole("button", { name: "取消" }));
    expect(commit).not.toHaveBeenCalled();
    expect(write).not.toHaveBeenCalled();
    expect(repair).not.toHaveBeenCalled();
    expect(close).toHaveBeenCalledOnce();
  });
  it("无需手动修复，模型调整只在确认时提交", async () => {
    const { commit } = mount(saved);
    const user = userEvent.setup();
    await screen.findByRole("checkbox", { name: "启用 gpt-4.1" });
    await user.click(screen.getByRole("checkbox", { name: "启用 gpt-4.1" }));
    await user.click(
      screen.getByRole("button", { name: "将 gpt-4.1 设为默认模型" }),
    );
    expect(screen.queryByRole("button", { name: /修复.*思考强度/ })).toBeNull();
    await user.click(screen.getByRole("button", { name: "保存并使用" }));
    expect(commit).toHaveBeenCalledWith(
      expect.objectContaining({
        id: "shared",
        apiKey: null,
        model: "gpt-4.1",
        codexOptions: expect.objectContaining({ models: expect.any(Array) }),
      }),
      true,
    );
  });
  it("失败保留表单、原 ID 和草稿；再次点击重试不会重复添加", async () => {
    const { commit, close } = mount(saved);
    commit.mockRejectedValueOnce(new Error("外部修改，未写入"));
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "保存并使用" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "外部修改，未写入",
    );
    expect(close).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "保存并使用" }));
    expect(commit).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({ id: "shared" }),
      true,
    );
    expect(close).toHaveBeenCalledOnce();
  });
  it("在 Claude 中编辑 Codex 创建的供应商，仍提交同一个共享 ID", async () => {
    const { commit } = mount(saved, "claude_desktop", false);
    await userEvent.click(screen.getByRole("button", { name: "保存修改" }));
    expect(commit).toHaveBeenCalledWith(
      expect.objectContaining({
        id: "shared",
        family: "codex",
        model: "gpt-5.4",
      }),
      false,
    );
  });
  it("自动识别 Claude 后用于 Codex，不需手动选协议；高级覆盖仍可用", async () => {
    vi.spyOn(api, "discoverConnection").mockResolvedValue(models("anthropic"));
    const { commit } = mount();
    const user = await fields();
    await user.click(screen.getByRole("button", { name: "添加并使用" }));
    expect(commit).toHaveBeenCalledWith(
      expect.objectContaining({
        model: "claude-sonnet-4-6",
        authMode: "x-api-key",
        codexOptions: expect.objectContaining({
          upstreamProtocol: "anthropic",
          fastMode: null,
        }),
      }),
      true,
    );
  });
  it("模型查询失败时保留表单且不提交", async () => {
    vi.spyOn(api, "discoverConnection").mockRejectedValue(
      new Error("HTTP 403"),
    );
    const { commit } = mount();
    const user = await fields();
    await user.click(screen.getByRole("button", { name: "添加并使用" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "此密钥没有访问权限",
    );
    expect(commit).not.toHaveBeenCalled();
  });
  it("新增密钥必填，字段错误与输入关联", async () => {
    const { commit } = mount();
    const user = userEvent.setup();
    await user.type(
      screen.getByLabelText(/API 地址/),
      "https://example.test/v1",
    );
    await user.click(screen.getByRole("button", { name: "添加并使用" }));
    expect(await screen.findByText("请填写 API Key")).toBeVisible();
    expect(screen.getByLabelText(/^API Key/)).toHaveAttribute(
      "aria-invalid",
      "true",
    );
    expect(commit).not.toHaveBeenCalled();
  });
  it("全部取消模型后不重新启用模型，也不提交", async () => {
    const { commit } = mount();
    const user = await fields();
    await screen.findByRole("checkbox", { name: "启用 gpt-5.4" });
    await user.click(screen.getByRole("checkbox", { name: "启用 gpt-5.4" }));
    await user.click(screen.getByRole("checkbox", { name: "启用 gpt-4.1" }));
    await user.click(screen.getByRole("button", { name: "刷新模型列表" }));
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "刷新模型列表" }),
      ).toBeEnabled(),
    );
    await user.click(screen.getByRole("button", { name: "添加并使用" }));
    expect(await screen.findByText("请至少启用一个模型")).toBeVisible();
    expect(commit).not.toHaveBeenCalled();
    expect(
      screen.getByRole("checkbox", { name: "启用 gpt-5.4" }),
    ).not.toBeChecked();
  });
  it("已启用模型的上下文非法时定位模型列表，不提交", async () => {
    const { commit } = mount(saved);
    const user = userEvent.setup();
    await screen.findByLabelText("上下文长度 gpt-5.4");
    await user.clear(screen.getByLabelText("上下文长度 gpt-5.4"));
    await user.type(screen.getByLabelText("上下文长度 gpt-5.4"), "0");
    await user.click(screen.getByRole("button", { name: "保存并使用" }));
    expect(await screen.findByText("请修正模型的上下文长度")).toBeVisible();
    expect(commit).not.toHaveBeenCalled();
  });
  it("只保存选项直接可见，界面始终只有一个提交按钮", async () => {
    const { commit } = mount();
    const user = await fields();
    await user.click(screen.getByLabelText("只保存供应商，暂不用于当前客户端"));
    await user.click(screen.getByRole("button", { name: "保存供应商" }));
    expect(commit).toHaveBeenCalledWith(expect.anything(), false);
  });
  it("等待模型时可以取消，迟到的结果不会落盘", async () => {
    let resolve!: (value: ModelSyncResult) => void;
    vi.spyOn(api, "discoverConnection").mockImplementation(
      () =>
        new Promise((r) => {
          resolve = r;
        }),
    );
    const { commit, close } = mount();
    const user = await fields();
    await user.click(screen.getByRole("button", { name: "添加并使用" }));
    expect(await screen.findByRole("button", { name: "取消" })).toBeEnabled();
    await user.click(screen.getByRole("button", { name: "取消" }));
    resolve(models());
    await waitFor(() => expect(close).toHaveBeenCalledOnce());
    expect(commit).not.toHaveBeenCalled();
  });
  it("当前配置文件写入失败仍保留表单，不误报成功", async () => {
    const commit = vi.fn().mockRejectedValue({
      code: "invalid_config",
      message: "当前配置文件格式损坏",
    });
    const complete = vi.fn(),
      close = vi.fn();
    render(
      <ProviderForm
        family="codex"
        target="codex"
        onCommit={commit}
        onComplete={complete}
        onClose={close}
      />,
    );
    const user = await fields();
    await user.click(screen.getByRole("button", { name: "添加并使用" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "当前配置文件格式损坏",
    );
    expect(commit).toHaveBeenCalledOnce();
    expect(complete).not.toHaveBeenCalled();
    expect(close).not.toHaveBeenCalled();
  });
  it("默认模型从最新列表消失时移除旧模型，保存最新列表与自动替代的默认值", async () => {
    const withModels = {
      ...saved,
      codexOptions: {
        fastMode: null,
        contextWindow: null,
        autoCompactTokenLimit: null,
        models: [
          {
            id: "gpt-5.4",
            enabled: true,
            contextWindow: 256000,
            reasoningEfforts: [],
          },
        ],
        modelsSyncedAt: 1,
        balanceQuery: null,
      },
    };
    vi.spyOn(api, "discoverConnection").mockResolvedValue({
      ...models(),
      models: models().models.filter((m) => m.id !== "gpt-5.4"),
    });
    const { commit } = mount(withModels);
    const user = userEvent.setup();
    await screen.findByLabelText("启用 gpt-4.1");
    await waitFor(() =>
      expect(screen.queryByLabelText("启用 gpt-5.4")).toBeNull(),
    );
    expect(screen.getByText("已启用 1 / 1")).toBeVisible();
    expect(screen.queryByLabelText("上下文长度 gpt-5.4")).toBeNull();
    await user.click(screen.getByRole("button", { name: "保存并使用" }));
    await waitFor(() => expect(commit).toHaveBeenCalledOnce());
    expect(commit.mock.calls[0][0].model).toBe("gpt-4.1");
    expect(
      commit.mock.calls[0][0].codexOptions.models.map(
        (m: { id: string }) => m.id,
      ),
    ).toEqual(["gpt-4.1", "text-embedding-3-large"]);
    expect(commit.mock.calls[0][1]).toBe(true);
  });
});
