import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import App from "./App";
import { api } from "./lib/api";
import type { Overview, Provider, TargetStatus } from "./types";
vi.mock("./components/ProviderBalance", () => ({
  ProviderBalance: () => <span>余额</span>,
}));
const provider: Provider = {
  id: "test",
  family: "codex",
  name: "测试供应商",
  baseUrl: "https://gateway.test/v1",
  model: "gpt-5.4",
  authMode: "bearer",
  hasKey: true,
  keySuffix: "test",
  updatedAt: 1,
  reasoningEffort: null,
};
const status: TargetStatus = {
  target: "codex",
  directory: "D:/isolated/codex",
  files: [],
  activeProviderId: null,
  state: "unmanaged",
  canRestore: false,
  message: "",
};
let data: Overview;
let client: QueryClient;
beforeEach(() => {
  localStorage.clear();
  vi.stubGlobal("matchMedia", () => ({
    matches: false,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }));
  data = {
    providers: [provider],
    targets: [status],
    dataDirectory: "D:/isolated/data",
  };
  vi.spyOn(api, "overview").mockImplementation(async () => data);
  vi.spyOn(api, "apply").mockResolvedValue({ ...status, state: "applied" });
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } },
  });
});
afterEach(() => {
  client.clear();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
function mount() {
  render(
    <QueryClientProvider client={client}>
      <App />
    </QueryClientProvider>,
  );
}
describe("供应商列表的下一步操作", () => {
  it("Codex 关闭 Claude 转换后禁止使用，开启仅保存偏好，下次点击使用才应用", async () => {
    data.providers = [{ ...provider, family: "claude" }];
    const conversion = vi
      .spyOn(api, "setProtocolConversion")
      .mockImplementation(async (expected, target, enabled) => {
        const updated: Provider = {
          ...expected,
          codexOptions: {
            fastMode: false,
            contextWindow: null,
            autoCompactTokenLimit: null,
            models: [],
            modelsSyncedAt: null,
            balanceQuery: null,
            conversionDisabledTargets: enabled ? [] : [target],
          },
        };
        data = { ...data, providers: [updated] };
        return { provider: updated, restored: false };
      });
    mount();
    const control = await screen.findByRole("switch", {
      name: "转换为 OpenAI · 测试供应商",
    });
    await userEvent.click(control);
    await waitFor(() => expect(control).not.toBeChecked());
    expect(conversion).toHaveBeenLastCalledWith(
      { ...provider, family: "claude" },
      "codex",
      false,
    );
    expect(screen.getByRole("button", { name: "使用" })).toBeDisabled();
    expect(api.apply).not.toHaveBeenCalled();
    await userEvent.click(control);
    await waitFor(() => expect(control).toBeChecked());
    expect(api.apply).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "使用" }));
    expect(api.apply).toHaveBeenCalledExactlyOnceWith("codex", provider.id);
  });
  it("转换保存失败不改变开关，保存期间禁止重复点击并显示保存状态", async () => {
    data.providers = [{ ...provider, family: "claude" }];
    let reject!: (reason: unknown) => void;
    const conversion = vi
      .spyOn(api, "setProtocolConversion")
      .mockImplementation(
        () =>
          new Promise((_resolve, fail) => {
            reject = fail;
          }),
      );
    mount();
    const control = await screen.findByRole("switch", {
      name: "转换为 OpenAI · 测试供应商",
    });
    await userEvent.click(control);
    expect(control).toBeChecked();
    expect(control).toBeDisabled();
    expect(screen.getAllByText("保存中…").length).toBeGreaterThan(0);
    await userEvent.click(control);
    expect(conversion).toHaveBeenCalledOnce();
    reject({ code: "external_change", message: "配置已被外部修改" });
    await waitFor(() => expect(control).toBeEnabled());
    expect(control).toBeChecked();
    expect(api.apply).not.toHaveBeenCalled();
    expect((await screen.findAllByText(/其他软件修改/))[0]).toBeVisible();
  });
  it("列表直接显示 Fast 开关，键盘开启和关闭均保存同一供应商，不切换供应商", async () => {
    const toggle = vi
      .spyOn(api, "setFastMode")
      .mockImplementation(async (_id, enabled) => {
        const updated = {
          ...provider,
          codexOptions: {
            fastMode: enabled,
            contextWindow: null,
            autoCompactTokenLimit: null,
            models: [],
            modelsSyncedAt: null,
            balanceQuery: null,
          },
        };
        data = { ...data, providers: [updated] };
        return { provider: updated, applied: false };
      });
    mount();
    const control = await screen.findByRole("switch", {
      name: "Fast 模式 · 测试供应商",
    });
    expect(control).not.toBeChecked();
    control.focus();
    await userEvent.keyboard(" ");
    await waitFor(() => expect(control).toBeChecked());
    expect(toggle).toHaveBeenLastCalledWith(provider.id, true);
    await userEvent.click(control);
    await waitFor(() => expect(control).not.toBeChecked());
    expect(toggle).toHaveBeenLastCalledWith(provider.id, false);
    expect(api.apply).not.toHaveBeenCalled();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
  it("Fast 保存时禁用重复操作，失败保留开关原值并显示错误", async () => {
    let reject!: (reason: unknown) => void;
    vi.spyOn(api, "setFastMode").mockImplementation(
      () =>
        new Promise((_resolve, fail) => {
          reject = fail;
        }),
    );
    mount();
    const control = await screen.findByRole("switch", {
      name: "Fast 模式 · 测试供应商",
    });
    await userEvent.click(control);
    expect(control).toBeDisabled();
    expect(control).not.toBeChecked();
    reject({ code: "external_change", message: "配置已被外部修改" });
    await waitFor(() => expect(control).toBeEnabled());
    expect(control).not.toBeChecked();
    expect((await screen.findAllByText(/其他软件修改/))[0]).toBeVisible();
    expect(api.apply).not.toHaveBeenCalled();
  });
  it("Claude 协议的供应商 Fast 禁用并说明原因", async () => {
    data.providers = [{ ...provider, family: "claude" }];
    const toggle = vi.spyOn(api, "setFastMode");
    mount();
    const control = await screen.findByRole("switch", {
      name: "Fast 模式 · 测试供应商",
    });
    expect(control).toBeDisabled();
    expect(screen.getByText("Claude 转换暂不支持")).toBeVisible();
    expect(control).toHaveAccessibleDescription("Claude 转换暂不支持");
    await userEvent.click(control);
    expect(toggle).not.toHaveBeenCalled();
  });
  it("主页面和添加流程不显示配置目录，目录只在设置可见", async () => {
    mount();
    await screen.findByText("测试供应商");
    expect(screen.queryByText(status.directory)).not.toBeInTheDocument();
    expect(screen.queryByText("确认配置位置")).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "添加供应商" }));
    expect(screen.queryByText(status.directory)).not.toBeInTheDocument();
    expect(screen.queryByText("确认配置位置")).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "取消" }));
    await userEvent.click(screen.getByRole("button", { name: "设置" }));
    expect(screen.getByText(status.directory)).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "自动查找" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "修改目录" })).toBeEnabled();
  });
  it("手动修改目录需保存，取消返回设置而不改变目录", async () => {
    const change = vi
      .spyOn(api, "directory")
      .mockImplementation(async (_target, directory) => {
        data = { ...data, targets: [{ ...status, directory }] };
      });
    mount();
    await userEvent.click(await screen.findByRole("button", { name: "设置" }));
    await userEvent.click(screen.getByRole("button", { name: "修改目录" }));
    const user = userEvent.setup();
    await user.clear(screen.getByLabelText("配置目录"));
    await user.type(screen.getByLabelText("配置目录"), "D:/isolated/other");
    await user.click(screen.getByRole("button", { name: "取消" }));
    expect(change).not.toHaveBeenCalled();
    expect(screen.getByText(status.directory)).toBeVisible();
    await user.click(screen.getByRole("button", { name: "修改目录" }));
    await user.clear(screen.getByLabelText("配置目录"));
    await user.type(screen.getByLabelText("配置目录"), "D:/isolated/confirmed");
    await user.click(screen.getByRole("button", { name: "保存目录" }));
    expect(await screen.findByText("D:/isolated/confirmed")).toBeVisible();
    expect(change).toHaveBeenCalledExactlyOnceWith(
      "codex",
      "D:/isolated/confirmed",
    );
  });
  it("已接管目录在设置说明恢复条件，禁止直接更换", async () => {
    data.targets = [
      {
        ...status,
        canRestore: true,
        activeProviderId: provider.id,
        state: "applied",
      },
    ];
    mount();
    await userEvent.click(await screen.findByRole("button", { name: "设置" }));
    expect(screen.getByRole("button", { name: "修改目录" })).toBeDisabled();
    expect(
      screen.getByText("当前目录已接管，更换前请先恢复原配置。"),
    ).toBeVisible();
  });
  it("首次读取失败提供重试，不显示空列表或允许添加", async () => {
    vi.spyOn(api, "overview").mockRejectedValue({
      code: "database",
      message: "isolated read failure",
    });
    mount();
    expect(await screen.findByText("暂时无法读取供应商")).toBeVisible();
    expect(screen.queryByText("还没有 API 供应商")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "添加第一个供应商" }),
    ).not.toBeInTheDocument();
    vi.mocked(api.overview).mockResolvedValue(data);
    await userEvent.click(screen.getByRole("button", { name: "重新读取" }));
    expect(await screen.findByText("测试供应商")).toBeVisible();
  });
  it("刷新失败保留列表，没有成功提示", async () => {
    mount();
    await screen.findByText("测试供应商");
    vi.mocked(api.overview).mockRejectedValue({
      code: "network",
      message: "无法刷新",
    });
    await userEvent.click(screen.getByRole("button", { name: "刷新配置状态" }));
    expect((await screen.findAllByText(/暂时无法连接供应商/))[0]).toBeVisible();
    expect(screen.queryByText("配置状态已刷新。")).not.toBeInTheDocument();
    expect(screen.getByText("测试供应商")).toBeVisible();
  });
  it("应用失败后的重试调用原应用操作", async () => {
    vi.spyOn(api, "apply")
      .mockRejectedValueOnce({ code: "network", message: "无法连接" })
      .mockResolvedValue({ ...status, state: "applied" });
    mount();
    await userEvent.click(await screen.findByRole("button", { name: "使用" }));
    await userEvent.click(screen.getByText("处理问题"));
    await userEvent.click(screen.getByRole("button", { name: "重试" }));
    await waitFor(() => expect(api.apply).toHaveBeenCalledTimes(2));
    expect(api.apply).toHaveBeenLastCalledWith("codex", provider.id);
  });
  it("列表打开独立模型弹窗，Escape取消并回到原入口，不打开完整编辑表单", async () => {
    mount();
    await userEvent.click(
      await screen.findByRole("button", { name: "配置 测试供应商 的模型" }),
    );
    expect(
      screen.getByRole("dialog", { name: "测试供应商 · 模型配置" }),
    ).toBeVisible();
    expect(screen.getByRole("region", { name: "本次选择" })).toHaveFocus();
    expect(
      screen.getByRole("button", {
        name: "配置 测试供应商 的模型",
        hidden: true,
      }),
    ).toHaveAttribute("aria-expanded", "true");
    expect(screen.queryByRole("combobox")).toBeNull();
    expect(screen.getByLabelText("启用 gpt-5.4")).toBeVisible();
    expect(screen.queryByLabelText("API 协议")).toBeNull();
    const save = vi.spyOn(api, "quickModels");
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).toBeNull();
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "配置 测试供应商 的模型" }),
      ).toHaveFocus(),
    );
    expect(save).not.toHaveBeenCalled();
  });
  it("确认模型弹窗后列表计数更新，取消勾选不改变已保存数量", async () => {
    const models = [
      "gpt-5.4",
      "gpt-4.1",
      "gpt-5.6",
      "gpt-6-sol",
      "gpt-6.1-sol",
    ].map((id) => ({
      id,
      enabled: true,
      contextWindow: 256000,
      reasoningEfforts: [],
    }));
    data.providers = [
      {
        ...provider,
        codexOptions: {
          models,
          fastMode: false,
          contextWindow: null,
          autoCompactTokenLimit: null,
          modelsSyncedAt: 1,
          balanceQuery: null,
        },
      },
    ];
    const save = vi
      .spyOn(api, "quickModels")
      .mockImplementation(async (input) => {
        const updated = {
          ...input.expected,
          model: input.model,
          codexOptions: {
            ...input.expected.codexOptions!,
            models: input.models,
          },
        };
        data = { ...data, providers: [updated] };
        return { provider: updated, applied: false };
      });
    mount();
    const control = await screen.findByRole("button", {
      name: "配置 测试供应商 的模型",
    });
    expect(control).toHaveTextContent("已启用 5 个");
    await userEvent.click(control);
    await userEvent.click(screen.getByLabelText("启用 gpt-4.1"));
    expect(screen.getByText("当前已保存 5 个，保存后启用 4 个")).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "取消" }));
    expect(save).not.toHaveBeenCalled();
    expect(control).toHaveTextContent("已启用 5 个");
    await userEvent.click(control);
    expect(screen.getByLabelText("启用 gpt-4.1")).toBeChecked();
    await userEvent.click(screen.getByLabelText("启用 gpt-4.1"));
    await userEvent.click(screen.getByRole("button", { name: "保存模型配置" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(control).toHaveTextContent("已启用 4 个");
    expect(save).toHaveBeenCalledOnce();
  });
  it("少量供应商也可Ctrl+F搜索并用Escape清空", async () => {
    mount();
    await screen.findByText("测试供应商");
    const user = userEvent.setup();
    await user.keyboard("{Control>}f{/Control}");
    const input = screen.getByRole("textbox", { name: "搜索 API 配置" });
    expect(input).toHaveFocus();
    await user.type(input, "unknown");
    expect(screen.getByText("没有找到匹配的配置")).toBeVisible();
    await user.keyboard("{Escape}");
    expect(input).toHaveValue("");
    expect(screen.getByText("测试供应商")).toBeVisible();
  });
});
