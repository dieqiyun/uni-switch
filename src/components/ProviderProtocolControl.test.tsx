import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { MockInstance } from "vitest";
import { ProviderProtocolControl } from "./ProviderProtocolControl";
import { api } from "../lib/api";
import type { Overview, Provider, Target } from "../types";

vi.mock("../lib/api", async (original) => ({
  ...(await original<typeof import("../lib/api")>()),
  desktopRuntime: true,
}));

const provider: Provider = {
  id: "protocol",
  family: "codex",
  name: "测试供应商",
  baseUrl: "https://gateway.test/v1",
  model: "claude-sonnet-4-5",
  authMode: "bearer",
  hasKey: true,
  keySuffix: "test",
  updatedAt: 1,
  reasoningEffort: null,
  codexOptions: {
    upstreamProtocol: "openai",
    protocolDetectedAt: 1,
    fastMode: false,
    contextWindow: null,
    autoCompactTokenLimit: null,
    models: [],
    modelsSyncedAt: null,
    balanceQuery: null,
  },
};
let client: QueryClient;
let probe: MockInstance<typeof api.detectProtocol>;
const toggle = vi.fn();
beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  probe = vi.spyOn(api, "detectProtocol").mockResolvedValue(provider);
  toggle.mockClear();
});
afterEach(() => {
  client.clear();
  vi.restoreAllMocks();
});
function view(value = provider, target: Target = "codex", props = {}) {
  return (
    <QueryClientProvider client={client}>
      <ProviderProtocolControl
        provider={value}
        target={target}
        active={false}
        disabled={false}
        saving={false}
        onToggle={toggle}
        {...props}
      />
    </QueryClientProvider>
  );
}
describe("供应商协议和转换开关", () => {
  it("根据已检测接口协议显示，不根据 Claude 模型名称推测，也不重复请求", () => {
    render(view());
    expect(screen.getByText("OpenAI")).toBeVisible();
    expect(screen.getByText("自动检测")).toBeVisible();
    expect(screen.getByText("直接接入，无需转换")).toBeVisible();
    expect(screen.queryByRole("switch")).toBeNull();
    expect(probe).not.toHaveBeenCalled();
  });
  it.each(["claude_desktop", "claude_cli"] as const)(
    "%s 的 OpenAI 协议默认转换为 Claude，键盘可操作",
    async (target) => {
      render(view(provider, target));
      const control = screen.getByRole("switch", {
        name: "转换为 Claude · 测试供应商",
      });
      expect(control).toBeChecked();
      expect(control).toHaveAccessibleDescription("已启用 · 使用时自动转换");
      control.focus();
      await userEvent.keyboard(" ");
      expect(toggle).toHaveBeenCalledExactlyOnceWith(provider, false);
    },
  );
  it("Codex 的 Claude 协议显示转换为 OpenAI；相同供应商在 Claude 原生接入", () => {
    const native = {
      ...provider,
      codexOptions: {
        ...provider.codexOptions!,
        upstreamProtocol: "anthropic" as const,
      },
    };
    const rendered = render(view(native));
    expect(screen.getByText("Claude")).toBeVisible();
    expect(
      screen.getByRole("switch", { name: "转换为 OpenAI · 测试供应商" }),
    ).toBeChecked();
    rendered.rerender(view(native, "claude_desktop"));
    expect(screen.queryByRole("switch")).toBeNull();
    expect(screen.getByText("直接接入，无需转换")).toBeVisible();
  });
  it("只关闭对应客户端，保存时禁用并解释当前使用供应商的关闭行为", () => {
    const value = {
      ...provider,
      codexOptions: {
        ...provider.codexOptions!,
        conversionDisabledTargets: ["claude_cli" as const],
      },
    };
    const rendered = render(view(value, "claude_cli"));
    expect(screen.getByRole("switch")).not.toBeChecked();
    rendered.rerender(
      view(value, "claude_desktop", { active: true, saving: true }),
    );
    const control = screen.getByRole("switch");
    expect(control).toBeChecked();
    expect(control).toBeDisabled();
    expect(control).toHaveAccessibleDescription(
      "已启用；关闭会停用并恢复原配置",
    );
    expect(screen.getByText("保存中…")).toBeVisible();
  });
  it("旧配置自动检测期间不显示未经确认的协议或开关，成功更新列表缓存", async () => {
    let resolve!: (value: Provider) => void;
    probe.mockImplementation(
      () =>
        new Promise<Provider>((done) => {
          resolve = done;
        }),
    );
    const legacy = { ...provider, codexOptions: undefined };
    const overview: Overview = {
      providers: [legacy],
      targets: [],
      dataDirectory: "isolated",
    };
    client.setQueryData(["overview"], overview);
    const rendered = render(view(legacy));
    expect(screen.getByText("检测中…")).toBeVisible();
    expect(screen.queryByText("OpenAI")).toBeNull();
    expect(screen.queryByRole("switch")).toBeNull();
    expect(probe).toHaveBeenCalledExactlyOnceWith(legacy);
    await act(async () => resolve(provider));
    await waitFor(() =>
      expect(client.getQueryData<Overview>(["overview"])?.providers[0]).toEqual(
        provider,
      ),
    );
    rendered.rerender(view(provider));
    expect(screen.getByText("OpenAI")).toBeVisible();
    expect(probe).toHaveBeenCalledOnce();
  });
  it("检测失败可重试，不误标协议；手动指定不会被自动检测覆盖", async () => {
    probe.mockRejectedValueOnce(new Error("暂时无法连接"));
    const legacy = { ...provider, codexOptions: undefined };
    const rendered = render(view(legacy));
    expect(await screen.findByText("协议检测失败")).toBeVisible();
    expect(screen.getByText("未确认")).toBeVisible();
    expect(screen.queryByRole("switch")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "重新检测" }));
    await waitFor(() => expect(probe).toHaveBeenCalledTimes(2));
    rendered.rerender(
      view({
        ...provider,
        codexOptions: {
          ...provider.codexOptions!,
          protocolPreference: "openai",
        },
      }),
    );
    expect(screen.getByText("手动指定")).toBeVisible();
    expect(screen.getByText("OpenAI")).toBeVisible();
    expect(probe).toHaveBeenCalledTimes(2);
  });
  it("同一秒的并发修改不会被迟到的协议检测结果覆盖", async () => {
    let resolve!: (value: Provider) => void;
    probe.mockImplementation(
      () =>
        new Promise<Provider>((done) => {
          resolve = done;
        }),
    );
    const legacy = { ...provider, codexOptions: undefined };
    client.setQueryData(["overview"], {
      providers: [legacy],
      targets: [],
      dataDirectory: "isolated",
    });
    render(view(legacy));
    const renamed = { ...legacy, name: "新名称" };
    client.setQueryData(["overview"], {
      providers: [renamed],
      targets: [],
      dataDirectory: "isolated",
    });
    await act(async () => resolve(provider));
    await waitFor(() => expect(screen.queryByText("检测中…")).toBeNull());
    expect(client.getQueryData<Overview>(["overview"])?.providers[0]).toEqual(
      renamed,
    );
  });
});
