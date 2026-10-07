import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useConnectionDiscovery } from "./useConnectionDiscovery";
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
const input = {
  providerId: null,
  baseUrl: "https://supplier.test/v1",
  apiKey: "test-key",
};
const response = (
  ids: string[],
  protocol: "openai" | "anthropic" = "openai",
): ModelSyncResult => ({
  protocol,
  authMode: protocol === "openai" ? "bearer" : "x-api-key",
  syncedAt: 123,
  models: ids.map((id) => ({
    id,
    enabled: true,
    contextWindow: null,
    reasoningEfforts: [],
  })),
});
const tick = () =>
  act(async () => {
    await vi.advanceTimersByTimeAsync(650);
  });

describe("自动接入的模型默认选择", () => {
  it.each([
    ["codex", "openai"],
    ["codex", "anthropic"],
    ["claude_desktop", "openai"],
    ["claude_desktop", "anthropic"],
    ["claude_cli", "openai"],
    ["claude_cli", "anthropic"],
  ] as const)(
    "%s 的 %s 模型首次同步全部启用，默认模型单独推荐",
    async (target, protocol) => {
      vi.useFakeTimers();
      const ids =
        protocol === "openai"
          ? ["gpt-5.4", "gpt-4.1", "qwen3-coder"]
          : ["claude-opus-4-6", "claude-sonnet-4-6", "claude-haiku-4-5"];
      vi.spyOn(api, "discoverConnection").mockResolvedValue(
        response(ids, protocol),
      );
      const hook = renderHook(() =>
        useConnectionDiscovery(input, "auto", target),
      );
      await tick();
      expect(
        hook.result.current.models.filter((m) => m.enabled).map((m) => m.id),
      ).toEqual(ids);
      expect(hook.result.current.model).toBe(
        protocol === "openai" ? "gpt-5.4" : "claude-sonnet-4-6",
      );
      if (target === "codex")
        expect(
          hook.result.current.models.every((m) => m.contextWindow === 256000),
        ).toBe(true);
    },
  );

  it("刷新期间的取消和上下文保留，新增模型默认启用，失败保留列表", async () => {
    vi.useFakeTimers();
    let finish!: (value: ModelSyncResult) => void;
    const probe = vi
      .spyOn(api, "discoverConnection")
      .mockResolvedValueOnce(response(["gpt-5.4", "gpt-4.1"]))
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            finish = resolve;
          }),
      )
      .mockRejectedValue(new Error("无权限"));
    const hook = renderHook(() =>
      useConnectionDiscovery(input, "auto", "codex"),
    );
    await tick();
    let refresh!: ReturnType<typeof hook.result.current.refresh>;
    act(() => {
      refresh = hook.result.current.refresh();
    });
    act(() => {
      hook.result.current.toggle("gpt-5.4", false);
      hook.result.current.setContext("gpt-4.1", "512");
    });
    await act(async () => {
      finish(response(["gpt-5.4", "gpt-4.1", "glm-5"]));
      await refresh;
    });
    expect(hook.result.current.models.map((m) => m.enabled)).toEqual([
      false,
      true,
      true,
    ]);
    expect(hook.result.current.model).toBe("gpt-4.1");
    expect(hook.result.current.contexts["gpt-4.1"]).toBe("512");
    expect(hook.result.current.models[1].contextWindow).toBe(512000);
    const before = hook.result.current.models;
    await act(async () => {
      await hook.result.current.refresh();
    });
    expect(hook.result.current.models).toEqual(before);
    expect(probe).toHaveBeenCalledTimes(3);
  });

  it("已保存取消项不被覆盖，全取消后刷新不偷偷启用默认模型", async () => {
    vi.useFakeTimers();
    const provider: Provider = {
      id: "saved",
      family: "codex",
      name: "Saved",
      baseUrl: input.baseUrl,
      authMode: "bearer",
      model: "gpt-5.4",
      hasKey: true,
      keySuffix: "test",
      updatedAt: 123,
      reasoningEffort: null,
      codexOptions: {
        models: response(["gpt-5.4", "gpt-4.1"]).models.map((m) => ({
          ...m,
          enabled: m.id === "gpt-5.4",
        })),
        fastMode: null,
        contextWindow: null,
        autoCompactTokenLimit: null,
        modelsSyncedAt: 123,
        balanceQuery: null,
      },
    };
    vi.spyOn(api, "discoverConnection").mockResolvedValue(
      response(["gpt-5.4", "gpt-4.1", "glm-5"]),
    );
    const hook = renderHook(() =>
      useConnectionDiscovery(
        { ...input, providerId: provider.id, apiKey: null },
        "auto",
        "codex",
        provider,
      ),
    );
    await tick();
    expect(hook.result.current.models.map((m) => m.enabled)).toEqual([
      true,
      false,
      true,
    ]);
    act(() => {
      hook.result.current.toggle("gpt-5.4", false);
      hook.result.current.toggle("glm-5", false);
    });
    await act(async () => {
      await hook.result.current.refresh();
    });
    expect(hook.result.current.models.every((m) => !m.enabled)).toBe(true);
    expect(hook.result.current.model).toBe("");
  });

  it.each(["codex", "claude_cli"] as const)(
    "%s 同步移除下架模型及其上下文，自动选择可用默认值，仍保留勾选与长度；失败不删除",
    async (target) => {
      vi.useFakeTimers();
      vi.spyOn(api, "discoverConnection")
        .mockResolvedValueOnce(
          response(["gpt-old", "gpt-kept", "gpt-off", "gpt-retired-off"]),
        )
        .mockResolvedValueOnce(response(["gpt-off", "gpt-kept", "gpt-new"]))
        .mockRejectedValue(new Error("同步失败"));
      const hook = renderHook(() =>
        useConnectionDiscovery(input, "auto", target),
      );
      await tick();
      act(() => {
        hook.result.current.selectDefault("gpt-old");
        hook.result.current.setContext("gpt-old", "1024");
        hook.result.current.setContext("gpt-kept", "512");
        hook.result.current.toggle("gpt-off", false);
        hook.result.current.toggle("gpt-retired-off", false);
      });
      await act(async () => {
        await hook.result.current.refresh();
      });
      expect(hook.result.current.models.map((m) => [m.id, m.enabled])).toEqual([
        ["gpt-off", false],
        ["gpt-kept", true],
        ["gpt-new", true],
      ]);
      expect(hook.result.current.model).toBe("gpt-kept");
      expect(Object.keys(hook.result.current.contexts).sort()).toEqual([
        "gpt-kept",
        "gpt-new",
        "gpt-off",
      ]);
      if (target === "codex") {
        expect(hook.result.current.contexts["gpt-kept"]).toBe("512");
        expect(hook.result.current.models[1].contextWindow).toBe(512000);
      }
      act(() => hook.result.current.selectDefault("gpt-old"));
      expect(hook.result.current.model).toBe("gpt-kept");
      const before = hook.result.current.models;
      await act(async () => {
        await hook.result.current.refresh();
      });
      expect(hook.result.current.models).toEqual(before);
      expect(hook.result.current.failure).toBe("同步失败");
    },
  );

  it("换站点后新的上游列表重新全选，迟到旧响应不串入模型", async () => {
    vi.useFakeTimers();
    let finish!: (value: ModelSyncResult) => void;
    vi.spyOn(api, "discoverConnection")
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            finish = resolve;
          }),
      )
      .mockResolvedValue(response(["new-model-a", "new-model-b"]));
    const hook = renderHook(
      ({ baseUrl }) =>
        useConnectionDiscovery({ ...input, baseUrl }, "auto", "codex"),
      { initialProps: { baseUrl: input.baseUrl } },
    );
    await tick();
    hook.rerender({ baseUrl: "https://new.test/v1" });
    await tick();
    await act(async () => {
      finish(response(["old-model"]));
    });
    expect(
      hook.result.current.models.filter((m) => m.enabled).map((m) => m.id),
    ).toEqual(["new-model-a", "new-model-b"]);
  });
});

it("刷新完成前的手动图片能力保留；恢复自动立即采用最新上游值", async () => {
  vi.useFakeTimers();
  let finish!: (v: ModelSyncResult) => void;
  vi.spyOn(api, "discoverConnection")
    .mockResolvedValueOnce(response(["custom-model"]))
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
  const hook = renderHook(() => useConnectionDiscovery(input, "auto", "codex"));
  await tick();
  let pending!: Promise<unknown>;
  act(() => {
    pending = hook.result.current.refresh();
  });
  act(() => {
    hook.result.current.setCapability("custom-model", "imageInput", true);
  });
  await act(async () => {
    const result = response(["custom-model"]);
    result.models[0].capabilities = { imageInput: false };
    finish(result);
    await pending;
  });
  expect(hook.result.current.models[0].capabilityOverrides?.imageInput).toBe(
    true,
  );
  expect(hook.result.current.models[0].capabilities?.imageInput).toBe(false);
  act(() => hook.result.current.resetCapabilities("custom-model"));
  expect(hook.result.current.models[0].capabilityOverrides).toEqual({});
});
