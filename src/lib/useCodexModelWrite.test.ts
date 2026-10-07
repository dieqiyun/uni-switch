import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "./api";
import { useCodexModelWrite } from "./useCodexModelWrite";
import type { ModelWriteInput } from "../types";
vi.mock("./api", async (original) => ({
  ...(await original<typeof import("./api")>()),
  desktopRuntime: true,
}));
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});
const input: ModelWriteInput = {
  connection: {
    providerId: "saved",
    baseUrl: "https://site.test/v1",
    apiKey: null,
  },
  authMode: "bearer",
  model: "a",
  models: [
    { id: "a", contextWindow: 256000, enabled: true, reasoningEfforts: [] },
  ],
  syncedAt: 123,
};
const tick = async () => {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(500);
  });
};
describe("Codex 模型自动写入", () => {
  it("仅写入当前供应商，合并快速编辑，暂停时取消待写入操作", async () => {
    vi.useFakeTimers();
    const write = vi
      .spyOn(api, "writeModels")
      .mockResolvedValue({ provider: {} as never, applied: true });
    const hook = renderHook(
      ({ value, enabled, paused }) =>
        useCodexModelWrite(value, enabled, paused),
      { initialProps: { value: input, enabled: false, paused: false } },
    );
    await tick();
    expect(write).not.toHaveBeenCalled();
    hook.rerender({ value: input, enabled: true, paused: false });
    const changed = {
      ...input,
      models: input.models.map((m) => ({ ...m, contextWindow: 512000 })),
    };
    hook.rerender({ value: changed, enabled: true, paused: false });
    await tick();
    expect(write).toHaveBeenCalledTimes(1);
    expect(write).toHaveBeenCalledWith(changed);
    hook.rerender({ value: input, enabled: true, paused: false });
    hook.rerender({ value: input, enabled: true, paused: true });
    await tick();
    expect(write).toHaveBeenCalledTimes(1);
  });
  it("写入失败显示原因并可重试，未应用结果不会显示成功", async () => {
    vi.useFakeTimers();
    const updated = vi.fn();
    vi.spyOn(api, "writeModels")
      .mockRejectedValueOnce(new Error("外部修改"))
      .mockResolvedValueOnce({ provider: {} as never, applied: false })
      .mockResolvedValue({ provider: {} as never, applied: true });
    const hook = renderHook(() =>
      useCodexModelWrite(input, true, false, updated),
    );
    await tick();
    expect(hook.result.current.failure).toBe("外部修改");
    expect(hook.result.current.written).toBe(false);
    act(() => hook.result.current.retry());
    await tick();
    expect(hook.result.current.failure).toContain("当前供应商已切换");
    act(() => hook.result.current.retry());
    await tick();
    expect(hook.result.current.written).toBe(true);
    expect(updated).toHaveBeenCalledOnce();
  });
});
