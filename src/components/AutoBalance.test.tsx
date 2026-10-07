import { act, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AutoBalance } from "./AutoBalance";
import { ProviderBalance } from "./ProviderBalance";
import { api } from "../lib/api";
import { invalidateBalance } from "../lib/balanceCache";
import type { BalanceResult, Provider } from "../types";
vi.mock("../lib/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../lib/api")>()),
  desktopRuntime: true,
}));
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});
const result = (amount: number): BalanceResult => ({
  amount,
  unit: "USD",
  scope: "账户余额",
  checkedAt: 123,
  providerType: "Sub2API",
});
const tick = async (ms = 650) => {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
};

describe("自动余额查询", () => {
  it("不支持的站点自动降低查询频率，手动刷新仍立即查询", async () => {
    vi.useFakeTimers();
    const balance = vi.spyOn(api, "balance").mockRejectedValue({
      code: "balance_unsupported",
      message: "站点未开放查询",
    });
    const provider: Provider = {
      id: "unsupported-backoff",
      family: "codex",
      name: "unsupported",
      baseUrl: "https://unsupported.test/v1",
      model: "gpt",
      authMode: "bearer",
      reasoningEffort: null,
      hasKey: true,
      keySuffix: "test",
      updatedAt: 1,
    };
    render(<ProviderBalance provider={provider} />);
    await tick(0);
    await tick(180000);
    expect(balance).toHaveBeenCalledOnce();
    expect(screen.getByText("暂不支持余额查询")).toBeInTheDocument();
    await act(async () => {
      screen.getByLabelText("刷新 unsupported 余额").click();
    });
    expect(balance).toHaveBeenCalledTimes(2);
  });
  it("供应商列表在前台每分钟刷新，重新聚焦复用缓存或刷新过期结果", async () => {
    vi.useFakeTimers();
    const balance = vi.spyOn(api, "balance").mockResolvedValue(result(7));
    const provider: Provider = {
      id: "periodic-balance",
      family: "codex",
      name: "periodic",
      baseUrl: "https://periodic.test/v1",
      model: "gpt",
      authMode: "bearer",
      reasoningEffort: null,
      hasKey: true,
      keySuffix: "key",
      updatedAt: 1,
    };
    render(<ProviderBalance provider={provider} />);
    await tick(0);
    expect(balance).toHaveBeenCalledTimes(1);
    await tick(60000);
    expect(balance).toHaveBeenCalledTimes(2);
    await act(async () => {
      window.dispatchEvent(new Event("focus"));
    });
    expect(balance).toHaveBeenCalledTimes(2);
    await tick(31000);
    await act(async () => {
      window.dispatchEvent(new Event("focus"));
    });
    expect(balance).toHaveBeenCalledTimes(3);
  });
  it("地址与密钥完整后才查询，连续输入合并为一次请求", async () => {
    vi.useFakeTimers();
    const balance = vi.spyOn(api, "balance").mockResolvedValue(result(3));
    const view = render(
      <AutoBalance
        input={{ providerId: null, baseUrl: "https://", apiKey: "" }}
        query={null}
      />,
    );
    await tick();
    expect(balance).not.toHaveBeenCalled();
    view.rerender(
      <AutoBalance
        input={{
          providerId: null,
          baseUrl: "https://first.test/v1",
          apiKey: "key-a",
        }}
        query={null}
      />,
    );
    await tick(300);
    view.rerender(
      <AutoBalance
        input={{
          providerId: null,
          baseUrl: "https://final.test/v1",
          apiKey: "key-b",
        }}
        query={null}
      />,
    );
    await tick(650);
    expect(balance).toHaveBeenCalledTimes(1);
    expect(balance).toHaveBeenCalledWith(
      expect.objectContaining({
        baseUrl: "https://final.test/v1",
        apiKey: "key-b",
      }),
      null,
    );
    expect(screen.getByText("账户余额 · 3 USD")).toBeVisible();
  });
  it("慢速旧响应不能覆盖更换地址后的余额", async () => {
    vi.useFakeTimers();
    let resolveOld!: (v: BalanceResult) => void;
    vi.spyOn(api, "balance")
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            resolveOld = resolve;
          }),
      )
      .mockResolvedValue(result(9));
    const view = render(
      <AutoBalance
        input={{
          providerId: null,
          baseUrl: "https://old.test/v1",
          apiKey: "old-key",
        }}
        query={null}
      />,
    );
    await tick();
    view.rerender(
      <AutoBalance
        input={{
          providerId: null,
          baseUrl: "https://new.test/v1",
          apiKey: "new-key",
        }}
        query={null}
      />,
    );
    await tick();
    expect(screen.getByText("账户余额 · 9 USD")).toBeVisible();
    await act(async () => resolveOld(result(1)));
    expect(screen.queryByText("1 USD")).not.toBeInTheDocument();
    expect(screen.getByText("账户余额 · 9 USD")).toBeVisible();
  });
  it("自动加载多个供应商时最多三家并发，相同配置复用结果", async () => {
    vi.useFakeTimers();
    const pending: ((v: BalanceResult) => void)[] = [];
    const balance = vi
      .spyOn(api, "balance")
      .mockImplementation(
        () => new Promise((resolve) => pending.push(resolve)),
      );
    const provider = (id: string): Provider => ({
      id,
      family: "codex",
      name: id,
      baseUrl: "https://suppliers.test/v1",
      model: "model",
      authMode: "bearer",
      reasoningEffort: null,
      hasKey: true,
      keySuffix: "key",
      updatedAt: 222,
    });
    const view = render(
      <>
        {[0, 1, 2, 3, 4].map((i) => (
          <ProviderBalance key={i} provider={provider(`queue-test-${i}`)} />
        ))}
        <ProviderBalance provider={provider("queue-test-0")} />
      </>,
    );
    await tick(0);
    expect(balance).toHaveBeenCalledTimes(3);
    await act(async () => {
      pending[0](result(1));
    });
    expect(balance).toHaveBeenCalledTimes(4);
    await act(async () => {
      pending[1](result(2));
    });
    expect(balance).toHaveBeenCalledTimes(5);
    await act(async () => {
      pending.slice(2).forEach((resolve) => resolve(result(3)));
    });
    view.rerender(<ProviderBalance provider={provider("queue-test-0")} />);
    await tick(0);
    expect(balance).toHaveBeenCalledTimes(5);
    expect(
      screen.getByText("1 USD", { selector: ".balance-summary" }),
    ).toBeVisible();
  });
  it("保存配置后丢弃缓存，即使更新时间和密钥尾号相同", async () => {
    vi.useFakeTimers();
    const balance = vi
      .spyOn(api, "balance")
      .mockResolvedValueOnce(result(2))
      .mockResolvedValue(result(8));
    const provider: Provider = {
      id: "cache-invalidation-test",
      family: "codex",
      name: "test",
      baseUrl: "https://cache.test/v1",
      model: "model",
      authMode: "bearer",
      reasoningEffort: null,
      hasKey: true,
      keySuffix: "same",
      updatedAt: 100,
    };
    const view = render(<ProviderBalance provider={provider} />);
    await tick(0);
    expect(
      screen.getByText("2 USD", { selector: ".balance-summary" }),
    ).toBeVisible();
    invalidateBalance(provider.id);
    view.rerender(<ProviderBalance provider={{ ...provider }} />);
    await tick(0);
    expect(balance).toHaveBeenCalledTimes(2);
    expect(
      screen.getByText("8 USD", { selector: ".balance-summary" }),
    ).toBeVisible();
  });
});
