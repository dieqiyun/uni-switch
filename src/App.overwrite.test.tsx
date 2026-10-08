import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import App from "./App";
import { api } from "./lib/api";
import type {
  ApplyOverwriteConfirmation,
  Overview,
  Provider,
  Target,
} from "./types";

vi.mock("./lib/api", async (original) => ({
  ...(await original<typeof import("./lib/api")>()),
  desktopRuntime: true,
}));
vi.mock("./components/WindowChrome", () => ({ WindowChrome: () => null }));
vi.mock("./components/ProviderBalance", () => ({
  ProviderBalance: () => <span>余额</span>,
}));
const provider: Provider = {
  id: "test",
  family: "codex",
  name: "测试供应商",
  baseUrl: "https://gateway.test/v1",
  model: "gpt-test",
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
let data: Overview, client: QueryClient;
function confirmation(
  target: Target = "codex",
  token = "one-time-approval",
): ApplyOverwriteConfirmation {
  return {
    target,
    providerId: provider.id,
    token,
    directory: `D:/isolated/${target}`,
    files: [`D:/isolated/${target}/config-file`],
  };
}
beforeEach(() => {
  localStorage.clear();
  vi.stubGlobal("matchMedia", () => ({
    matches: false,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }));
  data = {
    providers: [provider],
    dataDirectory: "D:/isolated/data",
    targets: (["codex", "claude_desktop", "claude_cli"] as const).map(
      (target) => ({
        target,
        directory: `D:/isolated/${target}`,
        files: [],
        activeProviderId: provider.id,
        state: "external_change",
        canRestore: true,
        message: "API 配置已被其他工具修改。",
        configurationRevision: 1,
      }),
    ),
  };
  vi.spyOn(api, "overview").mockImplementation(async () =>
    structuredClone(data),
  );
  vi.spyOn(api, "background").mockResolvedValue({
    supported: false,
    enabled: false,
  });
  vi.spyOn(api, "runtime").mockImplementation(async (target) => ({
    target,
    clientRunning: false,
    restartRequired: false,
    bridgeRequired: false,
    bridgeHealthy: true,
    configurationRevision: 1,
  }));
  vi.spyOn(api, "updateSource").mockResolvedValue({
    currentVersion: "0.5.20",
    repository: null,
  });
  vi.spyOn(api, "apply").mockRejectedValue({
    code: "external_change",
    message: "外部修改",
  });
  vi.spyOn(api, "prepareOverwrite").mockImplementation(async (target) =>
    confirmation(target),
  );
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
const overwriteDialog = () =>
  screen.getByRole("dialog", { name: "覆盖现有 API 配置？" });

describe("点击使用时确认外部配置覆盖", () => {
  it.each(["codex", "claude_desktop", "claude_cli"] as const)(
    "%s 外部冲突仍可使用，取消不确认写入并恢复焦点",
    async (target) => {
      data.providers = [
        {
          ...provider,
          family: target === "codex" ? "codex" : "claude",
          codexOptions: {
            ...provider.codexOptions!,
            upstreamProtocol: target === "codex" ? "openai" : "anthropic",
          },
        },
      ];
      localStorage.setItem(
        "uni-switch-ui-target-v1",
        JSON.stringify({
          family: target === "codex" ? "codex" : "claude",
          claudeTarget: target,
        }),
      );
      mount();
      const use = await screen.findByRole("button", {
        name: "使用",
      });
      expect(use).toBeEnabled();
      await userEvent.click(use);
      const dialog = await screen.findByRole("dialog", {
        name: "覆盖现有 API 配置？",
      });
      expect(dialog).toHaveTextContent("测试供应商");
      expect(dialog).toHaveTextContent(`D:/isolated/${target}`);
      const cancel = within(dialog).getByRole("button", {
        name: "取消",
      });
      await waitFor(() => expect(cancel).toHaveFocus());
      expect(api.apply).toHaveBeenCalledExactlyOnceWith(target, provider.id);
      expect(api.prepareOverwrite).toHaveBeenCalledExactlyOnceWith(
        target,
        provider.id,
      );
      await userEvent.click(cancel);
      expect(screen.queryByRole("dialog")).toBeNull();
      expect(api.apply).toHaveBeenCalledOnce();
      await waitFor(() => expect(use).toHaveFocus());
      expect(
        data.targets.find((status) => status.target === target)?.state,
      ).toBe("external_change");
    },
  );
  it("冲突在按钮点击后才发现时也弹窗，确认成功按原写入流程提示重启", async () => {
    data.targets[0].state = "unmanaged";
    data.targets[0].activeProviderId = null;
    vi.mocked(api.apply).mockImplementation(async (target, _id, token) => {
      if (!token) throw { code: "external_change", message: "刚刚被修改" };
      data.targets[0] = {
        ...data.targets[0],
        state: "applied",
        activeProviderId: provider.id,
        configurationRevision: 2,
      };
      return data.targets[0];
    });
    mount();
    await userEvent.click(await screen.findByRole("button", { name: "使用" }));
    await screen.findByRole("dialog", { name: "覆盖现有 API 配置？" });
    await userEvent.click(
      within(overwriteDialog()).getByRole("button", {
        name: "强制覆盖并使用",
      }),
    );
    expect(
      await screen.findByRole("dialog", { name: "重启 Codex 使配置生效" }),
    ).toBeVisible();
    expect(api.apply).toHaveBeenLastCalledWith(
      "codex",
      provider.id,
      "one-time-approval",
    );
    expect(api.prepareOverwrite).toHaveBeenCalledOnce();
  });
  it.each(["claude_desktop", "claude_cli"] as const)(
    "%s 确认覆盖后提示正在运行的客户端重启，稍后重启不调用重启接口",
    async (target) => {
      data.providers = [
        {
          ...provider,
          family: "claude",
          codexOptions: {
            ...provider.codexOptions!,
            upstreamProtocol: "anthropic",
          },
        },
      ];
      localStorage.setItem(
        "uni-switch-ui-target-v1",
        JSON.stringify({ family: "claude", claudeTarget: target }),
      );
      vi.mocked(api.runtime).mockImplementation(async (value) => {
        const revision =
          data.targets.find((status) => status.target === value)!
            .configurationRevision || 1;
        return {
          target: value,
          clientRunning: value === target,
          restartRequired: value === target && revision === 2,
          desktopRunning: value === target && target === "claude_desktop",
          desktopRestartRequired:
            value === target && target === "claude_desktop" && revision === 2,
          canRestartClient: true,
          bridgeRequired: false,
          bridgeHealthy: true,
          configurationRevision: revision,
        };
      });
      vi.mocked(api.apply).mockImplementation(async (value, _id, token) => {
        if (!token) throw { code: "external_change", message: "外部修改" };
        const status = data.targets.find((status) => status.target === value)!;
        Object.assign(status, { state: "applied", configurationRevision: 2 });
        return status;
      });
      const restart = vi.spyOn(api, "restartClient");
      mount();
      await userEvent.click(
        await screen.findByRole("button", { name: "使用" }),
      );
      await screen.findByRole("dialog", { name: "覆盖现有 API 配置？" });
      await userEvent.click(
        within(overwriteDialog()).getByRole("button", {
          name: "强制覆盖并使用",
        }),
      );
      const name =
        target === "claude_desktop" ? "Claude Code 桌面端" : "Claude CLI";
      const prompt = await screen.findByRole("dialog", {
        name: `重启 ${name} 使配置生效`,
      });
      await userEvent.click(
        within(prompt).getByRole("button", { name: "稍后重启" }),
      );
      expect(restart).not.toHaveBeenCalled();
      expect(api.apply).toHaveBeenLastCalledWith(
        target,
        provider.id,
        "one-time-approval",
      );
    },
  );
  it("确认期间配置再次变化，刷新确认窗口而不自动再次覆盖", async () => {
    vi.mocked(api.prepareOverwrite)
      .mockResolvedValueOnce(confirmation())
      .mockResolvedValue(confirmation("codex", "new-approval"));
    vi.mocked(api.apply)
      .mockRejectedValueOnce({ code: "external_change", message: "冲突" })
      .mockRejectedValue({
        code: "overwrite_confirmation_changed",
        message: "再次修改",
      });
    mount();
    await userEvent.click(await screen.findByRole("button", { name: "使用" }));
    await screen.findByRole("dialog", { name: "覆盖现有 API 配置？" });
    await userEvent.click(
      within(overwriteDialog()).getByRole("button", {
        name: "强制覆盖并使用",
      }),
    );
    await waitFor(() => expect(api.prepareOverwrite).toHaveBeenCalledTimes(2));
    expect(
      await within(overwriteDialog()).findByRole("alert"),
    ).toHaveTextContent("请检查后再次确认");
    expect(api.apply).toHaveBeenCalledTimes(2);
    expect(
      screen.queryByRole("dialog", { name: "重启 Codex 使配置生效" }),
    ).toBeNull();
    await userEvent.click(
      within(overwriteDialog()).getByRole("button", {
        name: "强制覆盖并使用",
      }),
    );
    await waitFor(() =>
      expect(api.apply).toHaveBeenCalledWith(
        "codex",
        provider.id,
        "new-approval",
      ),
    );
  });
  it("确认请求处理中禁止重复点击、取消与 Escape，失败仍保留确认窗口", async () => {
    let reject!: (value: unknown) => void;
    vi.mocked(api.apply)
      .mockRejectedValueOnce({ code: "external_change", message: "冲突" })
      .mockImplementation(
        () =>
          new Promise((_resolve, fail) => {
            reject = fail;
          }),
      );
    mount();
    await userEvent.click(await screen.findByRole("button", { name: "使用" }));
    await screen.findByRole("dialog", { name: "覆盖现有 API 配置？" });
    await userEvent.click(
      within(overwriteDialog()).getByRole("button", {
        name: "强制覆盖并使用",
      }),
    );
    expect(
      within(overwriteDialog()).getByRole("button", { name: "正在覆盖…" }),
    ).toBeDisabled();
    expect(
      within(overwriteDialog()).getByRole("button", {
        name: "取消",
      }),
    ).toBeDisabled();
    await userEvent.keyboard("{Escape}");
    expect(overwriteDialog()).toBeVisible();
    reject({ code: "write_failed", message: "文件写入失败，已回滚" });
    expect(
      await within(overwriteDialog()).findByRole("alert"),
    ).toHaveTextContent("已回滚");
    expect(
      within(overwriteDialog()).getByRole("button", {
        name: "取消",
      }),
    ).toBeEnabled();
    expect(api.apply).toHaveBeenCalledTimes(2);
  });
});
