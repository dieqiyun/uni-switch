import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { clientConfigApi } from "../lib/clientConfigApi";
import { ClientConfigEditor, ConfigRestartNotice } from "./ClientConfigEditor";
import { ExtraClientPanel } from "./ExtraClientPanel";
import type { ClientConfigStatus, ConfigDocument, Provider } from "../types";
const status: ClientConfigStatus = {
  client: "zcode",
  directory: "C:/isolated/zcode",
  files: [
    {
      id: "file",
      path: "C:/isolated/zcode/provider_config.json",
      format: "json",
      exists: true,
    },
  ],
  activeProviderId: null,
  canRestore: false,
  state: "unmanaged",
  message: "未接管",
  revision: "snapshot",
};
const document: ConfigDocument = {
  client: "zcode",
  fileId: "file",
  path: status.files[0].path,
  format: "json",
  content: '{"schemaVersion":1}',
  revision: "file-revision",
  exists: true,
};
const provider: Provider = {
  id: "supplier",
  family: "claude",
  name: "合成供应商",
  baseUrl: "https://example.test/v1",
  model: "qa-model",
  authMode: "bearer",
  reasoningEffort: null,
  hasKey: true,
  keySuffix: "test",
  updatedAt: 1,
};
function wrapper({ children }: { children: React.ReactNode }) {
  const query = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={query}>{children}</QueryClientProvider>;
}
beforeEach(() => {
  vi.spyOn(clientConfigApi, "status").mockResolvedValue(status);
  vi.spyOn(clientConfigApi, "read").mockResolvedValue(document);
});
afterEach(() => vi.restoreAllMocks());
describe("客户端配置文件编辑", () => {
  it("打开列表不会读取密钥，选中文件默认只读，实际保存后通知重启", async () => {
    const saved = vi.fn();
    vi.spyOn(clientConfigApi, "save").mockResolvedValue({
      changed: true,
      backupPath: "C:/isolated/backups/config.json",
      configurationRevision: 12,
    });
    render(
      <ClientConfigEditor client="zcode" onClose={vi.fn()} onSaved={saved} />,
      { wrapper },
    );
    await screen.findByRole("button", { name: /provider_config.json/ });
    expect(clientConfigApi.read).not.toHaveBeenCalled();
    await userEvent.click(
      screen.getByRole("button", { name: /provider_config.json/ }),
    );
    const source = await screen.findByRole("textbox");
    expect(source).toHaveAttribute("readonly");
    await userEvent.click(screen.getByRole("button", { name: "开始编辑" }));
    await userEvent.clear(source);
    await userEvent.paste('{"schemaVersion":1,"config":{}}');
    await userEvent.click(screen.getByRole("button", { name: "保存配置" }));
    await waitFor(() => expect(saved).toHaveBeenCalledOnce());
    expect(clientConfigApi.save).toHaveBeenCalledWith(
      expect.objectContaining({
        fileId: "file",
        revision: "file-revision",
        content: '{"schemaVersion":1,"config":{}}',
      }),
    );
  });
  it("保存冲突保留编辑内容，关闭必须确认放弃，取消不通知重启", async () => {
    const saved = vi.fn(),
      close = vi.fn();
    vi.spyOn(clientConfigApi, "save").mockRejectedValue({
      code: "config_conflict",
      message: "文件被外部修改，本次未保存",
    });
    render(
      <ClientConfigEditor client="zcode" onClose={close} onSaved={saved} />,
      { wrapper },
    );
    await userEvent.click(
      await screen.findByRole("button", { name: /provider_config.json/ }),
    );
    await userEvent.click(
      await screen.findByRole("button", { name: "开始编辑" }),
    );
    const source = screen.getByRole("textbox");
    await userEvent.clear(source);
    await userEvent.paste('{"manual":true}');
    await userEvent.click(screen.getByRole("button", { name: "保存配置" }));
    await screen.findByText("文件被外部修改，本次未保存");
    expect(source).toHaveValue('{"manual":true}');
    expect(saved).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "关闭" }));
    expect(close).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "继续编辑" }));
    expect(source).toHaveValue('{"manual":true}');
    await userEvent.click(screen.getByRole("button", { name: "关闭" }));
    await userEvent.click(
      screen.getByRole("button", { name: "放弃未保存修改" }),
    );
    expect(close).toHaveBeenCalledOnce();
    expect(saved).not.toHaveBeenCalled();
  });
  it("后端判断无变化时不弹出重启", async () => {
    const saved = vi.fn();
    vi.spyOn(clientConfigApi, "save").mockResolvedValue({
      changed: false,
      backupPath: null,
      configurationRevision: 0,
    });
    render(
      <ClientConfigEditor client="zcode" onClose={vi.fn()} onSaved={saved} />,
      { wrapper },
    );
    await userEvent.click(
      await screen.findByRole("button", { name: /provider_config.json/ }),
    );
    await userEvent.click(
      await screen.findByRole("button", { name: "开始编辑" }),
    );
    await userEvent.type(screen.getByRole("textbox"), " ");
    await userEvent.click(screen.getByRole("button", { name: "保存配置" }));
    await screen.findByText("内容未变化，无需保存或重启。");
    expect(saved).not.toHaveBeenCalled();
  });
  it("重启提示显示备份与新开对话要求", () => {
    render(
      <ConfigRestartNotice
        client="workbuddy"
        result={{
          changed: true,
          backupPath: "C:/isolated/backups/config.json",
          configurationRevision: 12,
        }}
        onClose={vi.fn()}
      />,
    );
    expect(screen.getByText(/请完全退出并重新打开客户端/)).toBeVisible();
    expect(screen.getByText(/模型选择器中选择/)).toBeVisible();
    expect(screen.getByText("C:/isolated/backups/config.json")).toBeVisible();
  });
});
describe("新增客户端一键配置", () => {
  it.each(["zcode", "dsh", "workbuddy"] as const)(
    "%s 接入供应商并仅在实际写入时提示",
    async (client) => {
      vi.spyOn(clientConfigApi, "status").mockResolvedValue({
        ...status,
        client,
      });
      const apply = vi
        .spyOn(clientConfigApi, "apply")
        .mockResolvedValue({
          changed: true,
          backupPath: "backup",
          configurationRevision: 5,
        });
      const written = vi.fn();
      render(
        <ExtraClientPanel
          client={client}
          providers={[provider]}
          onEdit={vi.fn()}
          onWritten={written}
          onManage={vi.fn()}
          onBusy={vi.fn()}
        />,
        { wrapper },
      );
      await waitFor(() =>
        expect(screen.getByRole("button", { name: "一键配置" })).toBeEnabled(),
      );
      await userEvent.click(screen.getByRole("button", { name: "一键配置" }));
      expect(apply).toHaveBeenCalledWith(
        client,
        "supplier",
        client === "workbuddy" ? "chat_completions" : "messages",
        undefined,
      );
      await waitFor(() => expect(written).toHaveBeenCalledOnce());
    },
  );
  it("外部修改覆盖使用当前快照，取消不写入", async () => {
    const apply = vi
      .spyOn(clientConfigApi, "apply")
      .mockRejectedValueOnce({ code: "config_conflict", message: "发生冲突" })
      .mockResolvedValueOnce({
        changed: true,
        backupPath: "backup",
        configurationRevision: 5,
      });
    const written = vi.fn();
    render(
      <ExtraClientPanel
        client="zcode"
        providers={[provider]}
        onEdit={vi.fn()}
        onWritten={written}
        onManage={vi.fn()}
        onBusy={vi.fn()}
      />,
      { wrapper },
    );
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "一键配置" })).toBeEnabled(),
    );
    await userEvent.click(screen.getByRole("button", { name: "一键配置" }));
    await screen.findByRole("button", { name: "覆盖并应用" });
    expect(written).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "覆盖并应用" }));
    expect(apply).toHaveBeenLastCalledWith(
      "zcode",
      "supplier",
      "messages",
      "snapshot",
    );
    await waitFor(() => expect(written).toHaveBeenCalledOnce());
  });
});
