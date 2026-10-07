import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useModelDiscovery } from "./useModelDiscovery";
import { api } from "./api";
import type { ModelSyncResult, Provider } from "../types";
vi.mock("./api", async (original) => ({
  ...(await original<typeof import("./api")>()),
  desktopRuntime: true,
}));
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});
const result = (...ids: string[]): ModelSyncResult => ({
  syncedAt: 123,
  models: ids.map((id) => ({
    id,
    enabled: true,
    contextWindow: null,
    reasoningEfforts: [],
  })),
});
const tick = async (ms = 650) => {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
};
const input = {
  providerId: null,
  baseUrl: "https://supplier.test/v1",
  apiKey: "test-key",
};
describe("模型自动同步", () => {
  it("输入完整才查询，输入防抖合并请求，上游模型默认全选", async () => {
    vi.useFakeTimers();
    const sync = vi
      .spyOn(api, "syncModels")
      .mockResolvedValue(result("a", "b"));
    const hook = renderHook(
      ({ key }) =>
        useModelDiscovery({ ...input, apiKey: key }, "bearer", "codex"),
      { initialProps: { key: "" } },
    );
    await tick();
    expect(sync).not.toHaveBeenCalled();
    hook.rerender({ key: "key-a" });
    await tick(300);
    hook.rerender({ key: "key-b" });
    await tick();
    expect(sync).toHaveBeenCalledTimes(1);
    expect(hook.result.current.model).toBe("a");
    expect(hook.result.current.models.map((m) => m.enabled)).toEqual([
      true,
      true,
    ]);
  });
  it("允许切换默认模型，取消勾选时重新选择默认，全部取消后不保留默认值", async () => {
    vi.useFakeTimers();
    vi.spyOn(api, "syncModels").mockResolvedValue(result("a", "b"));
    const hook = renderHook(() => useModelDiscovery(input, "bearer", "codex"));
    await tick();
    act(() => {
      hook.result.current.toggle("b", true);
    });
    act(() => {
      hook.result.current.selectDefault("b");
    });
    expect(hook.result.current.model).toBe("b");
    act(() => hook.result.current.toggle("b", false));
    expect(hook.result.current.model).toBe("a");
    act(() => hook.result.current.toggle("a", false));
    expect(hook.result.current.model).toBe("");
  });
  it("更换连接丢弃旧模型及旧响应，同步期间仍可以编辑", async () => {
    vi.useFakeTimers();
    let finish!: (v: ModelSyncResult) => void;
    vi.spyOn(api, "syncModels")
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            finish = resolve;
          }),
      )
      .mockResolvedValue(result("new-model"));
    const hook = renderHook(
      ({ url }) =>
        useModelDiscovery({ ...input, baseUrl: url }, "bearer", "codex"),
      { initialProps: { url: input.baseUrl } },
    );
    await tick();
    expect(hook.result.current.busy).toBe(true);
    hook.rerender({ url: "https://changed.test/v1" });
    await tick();
    await act(async () => finish(result("old-model")));
    expect(hook.result.current.model).toBe("new-model");
    expect(hook.result.current.models.map((m) => m.id)).toEqual(["new-model"]);
  });
  it("刷新保留取消选择与默认值，新模型自动加入；失败保留原列表", async () => {
    vi.useFakeTimers();
    const sync = vi
      .spyOn(api, "syncModels")
      .mockResolvedValueOnce(result("a", "b"))
      .mockResolvedValueOnce(result("a", "b", "c"))
      .mockRejectedValue(new Error("无权限"));
    const hook = renderHook(() => useModelDiscovery(input, "bearer", "codex"));
    await tick();
    act(() => hook.result.current.toggle("a", false));
    act(() => hook.result.current.selectDefault("b"));
    act(() => hook.result.current.setContext("b", "512"));
    await act(async () => hook.result.current.refresh());
    expect(hook.result.current.model).toBe("b");
    expect(
      hook.result.current.models.find((m) => m.id === "b")?.contextWindow,
    ).toBe(512000);
    expect(
      hook.result.current.models.find((m) => m.id === "c")?.contextWindow,
    ).toBe(256000);
    expect(hook.result.current.models.map((m) => m.enabled)).toEqual([
      false,
      true,
      true,
    ]);
    await act(async () => hook.result.current.refresh());
    expect(hook.result.current.model).toBe("b");
    expect(hook.result.current.failure).toBe("无权限");
    expect(sync).toHaveBeenCalledTimes(3);
  });
  it("上下文编辑保留未完成值；连接变化清除旧长度并恢复 256k", async () => {
    vi.useFakeTimers();
    vi.spyOn(api, "syncModels").mockResolvedValue(result("a"));
    const hook = renderHook(
      ({ url }) =>
        useModelDiscovery({ ...input, baseUrl: url }, "bearer", "codex"),
      { initialProps: { url: input.baseUrl } },
    );
    await tick();
    act(() => hook.result.current.setContext("a", ""));
    expect(hook.result.current.invalidContexts).toEqual(["a"]);
    await act(async () => hook.result.current.refresh());
    expect(hook.result.current.contexts.a).toBe("");
    act(() => hook.result.current.setContext("a", "512.5"));
    expect(hook.result.current.models[0].contextWindow).toBe(512500);
    hook.rerender({ url: "https://changed.test/v1" });
    await tick();
    expect(hook.result.current.models[0].contextWindow).toBe(256000);
  });
  it("旧配置仅有默认模型时仍可编辑；Claude 桌面禁用不支持的模型", async () => {
    vi.useFakeTimers();
    const provider: Provider = {
      id: "saved",
      family: "claude",
      name: "old",
      baseUrl: input.baseUrl,
      model: "claude-sonnet-4-6",
      authMode: "x-api-key",
      reasoningEffort: null,
      hasKey: true,
      keySuffix: "key",
      updatedAt: 0,
    };
    vi.spyOn(api, "syncModels").mockResolvedValue(
      result("gpt-model", "claude-opus-4-6", "claude-sonnet-4-6"),
    );
    const hook = renderHook(() =>
      useModelDiscovery(
        { ...input, providerId: "saved", apiKey: null },
        "x-api-key",
        "claude_desktop",
        provider,
      ),
    );
    expect(hook.result.current.model).toBe(provider.model);
    await tick();
    expect(hook.result.current.models[0].enabled).toBe(false);
    act(() => hook.result.current.toggle("gpt-model", true));
    expect(hook.result.current.models[0].enabled).toBe(false);
  });
});
