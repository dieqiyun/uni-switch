import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { clientConfigApi } from "../lib/clientConfigApi";
import { ExtraClientPanel } from "./ExtraClientPanel";
import type { ClientConfigStatus, ExtraClient, Provider } from "../types";

const provider: Provider = {
  id: "supplier",
  family: "claude",
  name: "合成供应商",
  baseUrl: "https://synthetic.invalid",
  model: "fixture",
  authMode: "bearer",
  reasoningEffort: null,
  hasKey: true,
  keySuffix: "test",
  updatedAt: 1,
};
const status: ClientConfigStatus = {
  client: "alma",
  directory: "C:/isolated/alma",
  files: [
    {
      id: "api",
      path: "C:/isolated/alma/local-api",
      format: "alma",
      exists: false,
    },
  ],
  activeProviderId: null,
  canRestore: false,
  state: "unmanaged",
  message: "未接管",
  revision: "snapshot",
};
function show(client: ExtraClient) {
  const onWritten = vi.fn();
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <ExtraClientPanel
        client={client}
        providers={[provider]}
        onEdit={vi.fn()}
        onWritten={onWritten}
        onManage={vi.fn()}
        onBusy={vi.fn()}
      />
    </QueryClientProvider>,
  );
  return onWritten;
}
afterEach(() => vi.restoreAllMocks());

it("API资源不开放文本编辑，客户端只提供支持的协议", async () => {
  vi.spyOn(clientConfigApi, "status").mockResolvedValue(status);
  vi.spyOn(clientConfigApi, "apply").mockResolvedValue({
    changed: true,
    backupPath: "C:/isolated/backup.json",
    configurationRevision: 2,
  });
  const written = show("alma");
  await screen.findByText("未接管");
  expect(
    screen.getByRole("button", { name: /查看 \/ 编辑配置文件/ }),
  ).toBeDisabled();
  expect(screen.getByRole("combobox", { name: "接口协议" })).toBeDisabled();
  await userEvent.click(screen.getByRole("button", { name: "一键配置" }));
  await waitFor(() =>
    expect(clientConfigApi.apply).toHaveBeenCalledWith(
      "alma",
      "supplier",
      "chat_completions",
      undefined,
    ),
  );
  expect(written).toHaveBeenCalledOnce();
});

it("导入链接等待客户端确认，不显示已写入或触发重启", async () => {
  vi.spyOn(clientConfigApi, "status").mockResolvedValue({
    ...status,
    client: "cindy",
    files: [],
  });
  vi.spyOn(clientConfigApi, "apply").mockResolvedValue({
    changed: false,
    backupPath: null,
    configurationRevision: 2,
    importLink: "cindy://provider/import?synthetic",
  });
  const written = show("cindy");
  await userEvent.click(
    await screen.findByRole("button", { name: "生成导入链接" }),
  );
  expect(
    await screen.findByRole("textbox", { name: /客户端导入链接/ }),
  ).toHaveValue("cindy://provider/import?synthetic");
  expect(screen.getByText(/请复制链接并在系统中打开/)).toBeInTheDocument();
  expect(written).not.toHaveBeenCalled();
  expect(screen.queryByText("已写入")).not.toBeInTheDocument();
});

it("切换配置档案写入选定ID，接管后锁定档案", async () => {
  vi.spyOn(clientConfigApi, "status").mockResolvedValue({
    ...status,
    client: "hermes",
    files: [],
    profiles: [
      {
        id: "profile:work",
        name: "work",
        directory: "C:/isolated/hermes/profiles/work",
      },
    ],
    profile: "",
  });
  vi.spyOn(clientConfigApi, "profile").mockResolvedValue();
  show("hermes");
  const choice = await screen.findByRole("combobox", {
    name: "配置档案 / 账户",
  });
  await userEvent.selectOptions(choice, "profile:work");
  await waitFor(() =>
    expect(clientConfigApi.profile).toHaveBeenCalledWith(
      "hermes",
      "profile:work",
    ),
  );
});

it("位置发现失败时仍可手动修复配置目录", async () => {
  vi.spyOn(clientConfigApi, "status").mockRejectedValue(
    new Error("位置记录损坏"),
  );
  vi.spyOn(clientConfigApi, "directory").mockResolvedValue();
  show("dsbot");
  await screen.findByRole("alert");
  await userEvent.click(screen.getByRole("button", { name: "修改配置目录" }));
  await userEvent.type(
    screen.getByRole("textbox", { name: "配置目录（绝对路径）" }),
    "C:/isolated/dsbot",
  );
  await userEvent.click(screen.getByRole("button", { name: "保存目录" }));
  await waitFor(() =>
    expect(clientConfigApi.directory).toHaveBeenCalledWith(
      "dsbot",
      "C:/isolated/dsbot",
    ),
  );
});
