import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import App from "./App";
vi.mock("./components/WindowChrome", () => ({ WindowChrome: () => null }));
import { api } from "./lib/api";
import type { RuntimeStatus, Target, Overview } from "./types";
vi.mock("./lib/api", async (original) => ({
  ...(await original<typeof import("./lib/api")>()),
  desktopRuntime: true,
}));
vi.mock("./components/ProviderBalance", () => ({
  ProviderBalance: () => <span>余额</span>,
}));
const provider = {
  id: "provider",
  family: "codex" as const,
  name: "测试供应商",
  baseUrl: "https://api.test/v1",
  model: "gpt-test",
  hasKey: true,
  keySuffix: "test",
  updatedAt: 1,
  authMode: "bearer" as const,
  reasoningEffort: null,
  codexOptions: {
    upstreamProtocol: "openai" as const,
    protocolDetectedAt: 1,
    fastMode: false,
    contextWindow: null,
    autoCompactTokenLimit: null,
    models: [],
    modelsSyncedAt: null,
    balanceQuery: null,
  },
};
let runtime: RuntimeStatus;
let claudeRuntimes: Partial<Record<Target, RuntimeStatus>>;
let data: Overview;
let client: QueryClient;
beforeEach(() => {
  localStorage.clear();
  vi.stubGlobal("matchMedia", () => ({
    matches: false,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }));
  runtime = {
    target: "codex",
    clientRunning: true,
    restartRequired: false,
    bridgeRequired: false,
    bridgeHealthy: true,
    desktopRunning: true,
    desktopRestartRequired: false,
    canRestartDesktop: true,
    configurationRevision: 1,
  };
  claudeRuntimes = {};
  data = {
    providers: [provider],
    dataDirectory: "isolated",
    targets: [
      {
        target: "codex",
        directory: "D:/isolated",
        files: [],
        activeProviderId: provider.id,
        state: "applied",
        canRestore: true,
        message: "",
        configurationRevision: 1,
      },
      ...(["claude_desktop", "claude_cli"] as const).map((target) => ({
        target,
        directory: `D:/isolated/${target}`,
        files: [],
        activeProviderId: provider.id,
        state: "applied" as const,
        canRestore: true,
        message: "",
      })),
    ],
  };
  vi.spyOn(api, "overview").mockImplementation(async () => ({
    ...data,
    targets: data.targets.map((value) => ({ ...value })),
  }));
  vi.spyOn(api, "background").mockResolvedValue({
    enabled: false,
    supported: false,
  });
  vi.spyOn(api, "runtime").mockImplementation(async (target) =>
    target === "codex"
      ? { ...runtime }
      : {
          target,
          clientRunning: false,
          restartRequired: false,
          bridgeRequired: false,
          bridgeHealthy: true,
          ...claudeRuntimes[target],
        },
  );
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } },
  });
});

describe("Claude 两端的配置写入提示", () => {
  it("关闭正在使用的转换恢复该端配置并提示重启，稍后重启不改变其他端", async () => {
    localStorage.setItem(
      "uni-switch-ui-target-v1",
      JSON.stringify({ family: "claude", claudeTarget: "claude_cli" }),
    );
    vi.spyOn(api, "setProtocolConversion").mockImplementation(
      async (expected, target) => {
        data.providers = [
          {
            ...expected,
            codexOptions: {
              ...expected.codexOptions!,
              conversionDisabledTargets: [target],
            },
          },
        ];
        data.targets = data.targets.map((s) =>
          s.target === target
            ? {
                ...s,
                state: "unmanaged",
                activeProviderId: null,
                canRestore: false,
                configurationRevision: 2,
              }
            : s,
        );
        claudeRuntimes[target] = {
          target,
          clientRunning: true,
          restartRequired: true,
          bridgeRequired: false,
          bridgeHealthy: true,
          configurationRevision: 2,
          canRestartClient: true,
        };
        return { provider: data.providers[0], restored: true };
      },
    );
    const restart = vi.spyOn(api, "restartClient");
    mount();
    const control = await screen.findByRole("switch", {
      name: "转换为 Claude · 测试供应商",
    });
    await userEvent.click(control);
    expect(
      await screen.findByRole("dialog", { name: "重启 Claude CLI 使配置生效" }),
    ).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "稍后重启" }));
    expect(control).not.toBeChecked();
    expect(screen.getByRole("button", { name: "使用" })).toBeDisabled();
    expect(restart).not.toHaveBeenCalled();
    expect(
      data.targets.find((s) => s.target === "codex")?.activeProviderId,
    ).toBe(provider.id);
    expect(
      data.targets.find((s) => s.target === "claude_desktop")?.activeProviderId,
    ).toBe(provider.id);
  });
  it.each(["claude_desktop", "claude_cli"] as const)(
    "%s 恢复后提示、稍后不重复，并保留列表重启入口",
    async (target) => {
      localStorage.setItem(
        "uni-switch-ui-target-v1",
        JSON.stringify({ family: "claude", claudeTarget: target }),
      );
      const restart = vi.spyOn(api, "restartClient");
      vi.spyOn(api, "restore").mockImplementation(async () => {
        claudeRuntimes[target] = {
          target,
          clientRunning: true,
          restartRequired: true,
          bridgeRequired: false,
          bridgeHealthy: true,
          configurationRevision: 10,
          desktopRunning: target === "claude_desktop",
          desktopRestartRequired: target === "claude_desktop",
          canRestartClient: true,
        };
        return {} as never;
      });
      mount();
      await userEvent.click(
        await screen.findByRole("button", { name: "设置" }),
      );
      await userEvent.click(screen.getByRole("button", { name: "恢复原配置" }));
      await userEvent.click(screen.getByRole("button", { name: "恢复原配置" }));
      const name =
        target === "claude_desktop" ? "Claude Code 桌面端" : "Claude CLI";
      expect(
        await screen.findByRole("dialog", { name: `重启 ${name} 使配置生效` }),
      ).toBeVisible();
      await userEvent.click(screen.getByRole("button", { name: "稍后重启" }));
      await client.invalidateQueries({ queryKey: ["runtime"] });
      await waitFor(() =>
        expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
      );
      expect(restart).not.toHaveBeenCalled();
      await userEvent.click(
        screen.getByRole("button", { name: `重启 ${name}` }),
      );
      expect(
        screen.getByRole("dialog", { name: `重启 ${name} 使配置生效` }),
      ).toBeVisible();
    },
  );
  it("共享供应商同步后三端依次提示，同一版本在不同客户端分别记录", async () => {
    runtime = {
      ...runtime,
      configurationRevision: 20,
      desktopRestartRequired: true,
    };
    for (const target of ["claude_desktop", "claude_cli"] as const) {
      claudeRuntimes[target] = {
        ...runtime,
        target,
        restartRequired: true,
        desktopRunning: target === "claude_desktop",
        desktopRestartRequired: target === "claude_desktop",
      };
    }
    mount();
    for (const name of ["Codex", "Claude Code 桌面端", "Claude CLI"]) {
      expect(
        await screen.findByRole("dialog", { name: `重启 ${name} 使配置生效` }),
      ).toBeVisible();
      await userEvent.click(screen.getByRole("button", { name: "稍后重启" }));
    }
    await client.invalidateQueries({ queryKey: ["runtime"] });
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    claudeRuntimes.claude_cli!.configurationRevision = 21;
    await client.invalidateQueries({ queryKey: ["runtime"] });
    expect(
      await screen.findByRole("dialog", { name: "重启 Claude CLI 使配置生效" }),
    ).toBeVisible();
  });
  it("Claude未运行或没有真实写入时不提示", async () => {
    claudeRuntimes.claude_desktop = {
      ...runtime,
      target: "claude_desktop",
      desktopRunning: false,
      desktopRestartRequired: true,
    };
    claudeRuntimes.claude_cli = {
      ...runtime,
      target: "claude_cli",
      clientRunning: true,
      restartRequired: false,
    };
    mount();
    await screen.findByRole("button", { name: "设置" });
    await client.invalidateQueries({ queryKey: ["runtime"] });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
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
describe("写入后 Codex 重启提示", () => {
  it("未检测到桌面进程也在每次实际写入后提示，同次稍后不重复", async () => {
    runtime = {
      ...runtime,
      clientRunning: false,
      desktopRunning: false,
      canRestartDesktop: false,
    };
    vi.spyOn(api, "setFastMode").mockImplementation(async () => {
      const revision = ++data.targets[0].configurationRevision!;
      runtime = { ...runtime, configurationRevision: revision };
      return { provider, applied: true };
    });
    mount();
    for (let i = 0; i < 2; i++) {
      await userEvent.click(
        await screen.findByRole("switch", { name: "Fast 模式 · 测试供应商" }),
      );
      expect(
        await screen.findByRole("dialog", { name: "重启 Codex 使配置生效" }),
      ).toBeVisible();
      expect(
        screen.getByText(
          "Codex 配置已更新。请重启已打开的 Codex，使本次修改生效。",
        ),
      ).toBeVisible();
      expect(screen.getByRole("button", { name: "立即重启" })).toBeDisabled();
      await userEvent.click(screen.getByRole("button", { name: "稍后重启" }));
      await client.invalidateQueries({ queryKey: ["overview"] });
      await client.invalidateQueries({ queryKey: ["runtime"] });
      await waitFor(() =>
        expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
      );
    }
  });
  it("进程检查失败时仍依据成功写入弹窗，恢复原配置也提示", async () => {
    vi.spyOn(api, "runtime").mockRejectedValue({ message: "进程检查不可用" });
    vi.spyOn(api, "restore").mockImplementation(async () => {
      data.targets[0].configurationRevision = 2;
      return {} as never;
    });
    mount();
    await userEvent.click(await screen.findByRole("button", { name: "设置" }));
    await userEvent.click(screen.getByRole("button", { name: "恢复原配置" }));
    await userEvent.click(screen.getByRole("button", { name: "恢复原配置" }));
    expect(
      await screen.findByRole("dialog", { name: "重启 Codex 使配置生效" }),
    ).toBeVisible();
    expect(screen.getByText(/尚未确认可自动重启的 Codex/)).toBeVisible();
  });
  it("只保存或相同配置没有文件变化时不制造提示", async () => {
    vi.spyOn(api, "setFastMode").mockResolvedValue({
      provider,
      applied: false,
    });
    mount();
    await userEvent.click(
      await screen.findByRole("switch", { name: "Fast 模式 · 测试供应商" }),
    );
    await client.invalidateQueries({ queryKey: ["overview"] });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
  it("历史写入不会在启动时重新提示；目录更换也不作为配置修改", async () => {
    runtime = {
      ...runtime,
      desktopRunning: false,
      desktopRestartRequired: false,
    };
    data.targets[0].configurationRevision = 100;
    mount();
    await screen.findByRole("button", { name: "设置" });
    await client.invalidateQueries({ queryKey: ["overview"] });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    data.targets[0].directory = "D:/another-directory";
    data.targets[0].configurationRevision = 101;
    await client.invalidateQueries({ queryKey: ["overview"] });
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
  });
  it("当前Fast写入后弹窗，稍后不重启且轮询不重复弹出；下一次修改再次提示", async () => {
    vi.spyOn(api, "setFastMode").mockImplementation(async () => {
      runtime = {
        ...runtime,
        configurationRevision: runtime.configurationRevision! + 1,
        restartRequired: true,
        desktopRestartRequired: true,
      };
      data.targets[0].configurationRevision = runtime.configurationRevision;
      return { provider, applied: true };
    });
    const restart = vi.spyOn(api, "restartCodex");
    mount();
    await userEvent.click(
      await screen.findByRole("switch", { name: "Fast 模式 · 测试供应商" }),
    );
    expect(
      await screen.findByRole("dialog", { name: "重启 Codex 使配置生效" }),
    ).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "稍后重启" }));
    await client.invalidateQueries({ queryKey: ["runtime"] });
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    expect(restart).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "重启 Codex" })).toBeVisible();
    await userEvent.click(
      screen.getByRole("switch", { name: "Fast 模式 · 测试供应商" }),
    );
    expect(
      await screen.findByRole("dialog", { name: "重启 Codex 使配置生效" }),
    ).toBeVisible();
  });
  it("模型与上下文确认写入后提示，编辑时不提示且取消不写入", async () => {
    runtime = { ...runtime, desktopRunning: false, canRestartDesktop: false };
    vi.spyOn(api, "discoverConnection").mockResolvedValue({
      protocol: "openai",
      authMode: "bearer",
      baseUrl: provider.baseUrl,
      syncedAt: 123,
      models: [
        {
          id: provider.model,
          enabled: true,
          contextWindow: null,
          reasoningEfforts: [],
        },
      ],
    });
    vi.spyOn(api, "balance").mockRejectedValue({ message: "余额未配置" });
    const commit = vi
      .spyOn(api, "quickModels")
      .mockImplementation(async (input) => {
        data.targets[0].configurationRevision = 2;
        runtime.configurationRevision = 2;
        return { provider: { ...provider, model: input.model }, applied: true };
      });
    mount();
    await userEvent.click(
      await screen.findByRole("button", { name: "配置 测试供应商 的模型" }),
    );
    await userEvent.clear(screen.getByLabelText("上下文长度 gpt-test"));
    await userEvent.type(screen.getByLabelText("上下文长度 gpt-test"), "128");
    expect(
      screen.queryByRole("dialog", { name: "重启 Codex 使配置生效" }),
    ).toBeNull();
    expect(commit).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "取消" }));
    expect(commit).not.toHaveBeenCalled();
    await userEvent.click(
      screen.getByRole("button", { name: "配置 测试供应商 的模型" }),
    );
    await userEvent.clear(screen.getByLabelText("上下文长度 gpt-test"));
    await userEvent.type(screen.getByLabelText("上下文长度 gpt-test"), "128");
    await userEvent.click(screen.getByRole("button", { name: "保存并应用" }));
    expect(
      await screen.findByRole("dialog", { name: "重启 Codex 使配置生效" }),
    ).toBeVisible();
    expect(commit).toHaveBeenCalledOnce();
    expect(commit.mock.calls[0][0].models[0].contextWindow).toBe(128000);
  });
  it("仅CLI运行、没有真实写入或写入失败时不弹桌面重启", async () => {
    runtime = {
      ...runtime,
      desktopRunning: false,
      restartRequired: true,
      desktopRestartRequired: false,
    };
    vi.spyOn(api, "setFastMode").mockRejectedValue({
      code: "external_change",
      message: "外部修改",
    });
    mount();
    await userEvent.click(
      await screen.findByRole("switch", { name: "Fast 模式 · 测试供应商" }),
    );
    await waitFor(() => expect(screen.getByRole("switch")).toBeEnabled());
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "立即重启" }),
    ).not.toBeInTheDocument();
  });
  it("其他弹窗打开时等待，关闭后才展示重启提示", async () => {
    mount();
    await userEvent.click(await screen.findByRole("button", { name: "设置" }));
    runtime = {
      ...runtime,
      configurationRevision: 2,
      restartRequired: true,
      desktopRestartRequired: true,
    };
    await client.invalidateQueries({ queryKey: ["runtime"] });
    expect(screen.getByRole("dialog", { name: "Codex 设置" })).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "关闭弹窗" }));
    expect(
      await screen.findByRole("dialog", { name: "重启 Codex 使配置生效" }),
    ).toBeVisible();
  });
});

it("升级自动修复模型能力后首次读取即提示重启，没有运行进程也保留稍后选项", async () => {
  data.repairedModelCapabilities = true;
  data.targets[0].configurationRevision = 4;
  runtime.configurationRevision = 4;
  runtime.clientRunning = false;
  runtime.desktopRunning = false;
  runtime.canRestartDesktop = false;
  mount();
  expect(
    await screen.findByRole("dialog", { name: "重启 Codex 使配置生效" }),
  ).toBeVisible();
  await userEvent.click(screen.getByRole("button", { name: "稍后重启" }));
  await client.invalidateQueries({ queryKey: ["overview"] });
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
});
