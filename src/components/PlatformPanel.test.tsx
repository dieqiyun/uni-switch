import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { PlatformPanel } from "./PlatformPanel";
import { platformCall } from "../lib/platformApi";
import { api } from "../lib/api";
vi.mock("../lib/platformApi", () => ({ platformCall: vi.fn() }));
vi.mock("../lib/api", () => ({
  desktopRuntime: true,
  errorMessage: (e: Error) => e.message,
  api: { save: vi.fn() },
}));
const source = {
  id: "source",
  name: "Gemini test",
  baseUrl: "https://example.invalid/v1beta",
  protocol: "gemini",
  authMode: "bearer",
  enabled: true,
  vendor: "auto",
  models: [{ id: "gemini-test", kind: "chat" }],
};
const account = {
  id: "account",
  kind: "google",
  name: "Google test",
  user: "synthetic@example.invalid",
  enabled: true,
  active: true,
  project: "project",
};
const call = vi.mocked(platformCall);
function mount() {
  return render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <PlatformPanel />
    </QueryClientProvider>,
  );
}
beforeEach(() => {
  vi.clearAllMocks();
  call.mockImplementation(async (action) => {
    if (action === "accounts") return [account];
    if (action === "sources") return [source];
    if (action === "info") return { port: 12345, token: "synthetic-local-key" };
    return true;
  });
});
describe("订阅账号与统一网关", () => {
  it("Google 登录要求显式 OAuth 配置，提交后清除密码且切换服务不复用注册信息", async () => {
    call.mockImplementation(async (action) => {
      if (action === "accounts" || action === "sources") return [];
      if (action === "login")
        return { id: "google-login", kind: "google", state: "waiting" };
      return true;
    });
    mount();
    await userEvent.click(screen.getByText("订阅账号管理（0）"));
    await userEvent.selectOptions(screen.getByLabelText("订阅服务"), "google");
    const id = screen.getByLabelText("Google OAuth Client ID");
    const secret = screen.getByLabelText("Google OAuth Client Secret");
    expect(id).toBeRequired();
    expect(secret).toBeRequired();
    expect(secret).toHaveAttribute("type", "password");
    expect(secret).toHaveAccessibleDescription(/不内置第三方注册信息/);
    await userEvent.type(id, "synthetic-client");
    await userEvent.type(secret, "synthetic-secret");
    await userEvent.click(screen.getByRole("button", { name: "添加并登录" }));
    expect(call).toHaveBeenCalledWith("login", {
      kind: "google",
      name: "",
      project: "",
      oauth: { clientId: "synthetic-client", clientSecret: "synthetic-secret" },
    });
    expect(secret).toHaveValue("");
    await userEvent.selectOptions(
      screen.getByLabelText("订阅服务"),
      "antigravity",
    );
    expect(id).toHaveValue("");
    expect(secret).toHaveValue("");
  });
  it("原生 Gemini 来源通过独立表单保存，密钥输入有名称且不会回显已有密钥", async () => {
    mount();
    await screen.findByText("Gemini test");
    await userEvent.click(screen.getByText("网关来源（1）"));
    await userEvent.click(screen.getByRole("button", { name: "编辑" }));
    const key = screen.getByLabelText("API Key（留空保留）");
    expect(key).toHaveAttribute("type", "password");
    expect(key).toHaveValue("");
    expect(screen.getByLabelText("上游协议")).toHaveValue("gemini");
    await userEvent.type(screen.getByLabelText("来源名称"), " changed");
    await userEvent.click(screen.getByRole("button", { name: "保存来源" }));
    expect(await screen.findByRole("status")).toHaveTextContent(
      "网关来源已保存",
    );
    expect(call).toHaveBeenCalledWith("save", {
      profile: {
        ...source,
        name: "Gemini test changed",
        apiKey: "",
        models: source.models,
      },
    });
  });
  it("账号停用与 Cloud 项目保存走账号操作，来源可绑定当前账号", async () => {
    mount();
    await screen.findByText("Google test");
    await userEvent.click(screen.getByText("订阅账号管理（1）"));
    await userEvent.click(screen.getByRole("button", { name: "停用" }));
    expect(call).toHaveBeenCalledWith("accountEnable", {
      id: "account",
      value: "false",
    });
    const project = screen.getByLabelText("Cloud 项目 ID");
    await userEvent.clear(project);
    await userEvent.type(project, "new-project");
    await userEvent.click(screen.getByRole("button", { name: "保存项目" }));
    expect(call).toHaveBeenCalledWith("accountProject", {
      id: "account",
      value: "new-project",
    });
    const accounts = screen.getByText("订阅账号管理（1）").closest("details")!;
    await userEvent.click(
      within(accounts).getByRole("button", { name: "添加网关来源" }),
    );
    expect(screen.queryByLabelText("API Key")).toBeNull();
    expect(screen.getByLabelText("认证来源")).toHaveValue("account");
    await userEvent.selectOptions(screen.getByLabelText("认证来源"), "google");
    expect(screen.getByLabelText("认证来源")).toHaveValue("google");
  });
  it("登录失败提供持久错误，用户可取消等待中的授权", async () => {
    call.mockImplementation(async (action) => {
      if (action === "accounts" || action === "sources") return [];
      if (action === "login")
        return {
          id: "login",
          kind: "chatgpt",
          state: "waiting",
          url: "https://auth.openai.com/oauth/authorize",
        };
      if (action === "loginStatus")
        return {
          id: "login",
          kind: "chatgpt",
          state: "cancelled",
          message: "登录已取消",
        };
      return true;
    });
    mount();
    await userEvent.click(screen.getByText("订阅账号管理（0）"));
    await userEvent.click(screen.getByRole("button", { name: "添加并登录" }));
    expect(
      await screen.findByRole("button", { name: "打开授权页面" }),
    ).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "取消登录" }));
    expect(call).toHaveBeenCalledWith("loginCancel", { id: "login" });
    expect(await screen.findByText(/登录已取消/)).toBeVisible();
  });
  it("网关来源生成可应用的供应商配置，使用统一地址和来源限定模型", async () => {
    mount();
    await screen.findByText("Gemini test");
    await userEvent.click(screen.getByText("网关来源（1）"));
    await userEvent.click(
      screen.getByRole("button", { name: "添加到供应商列表" }),
    );
    expect(await screen.findByRole("status")).toHaveTextContent(
      "已添加到供应商列表",
    );
    expect(api.save).toHaveBeenCalledWith(
      expect.objectContaining({
        baseUrl: "http://127.0.0.1:12345/v1",
        apiKey: "synthetic-local-key",
        model: "source/gemini-test",
      }),
    );
  });
});
