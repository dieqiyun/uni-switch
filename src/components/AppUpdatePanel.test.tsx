import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AppUpdatePanel } from "./AppUpdatePanel";
import { useAppUpdate } from "../lib/useAppUpdate";
import { api } from "../lib/api";
import { APP_VERSION } from "../lib/appVersion";
import type { UpdateCheck } from "../types";

vi.mock("../lib/api", async (original) => ({
  ...(await original<typeof import("../lib/api")>()),
  desktopRuntime: true,
}));
const release: UpdateCheck = {
  currentVersion: APP_VERSION,
  latestVersion: "99.0.0",
  available: true,
  repository: "example/uni-switch",
  releaseUrl: "https://github.com/example/uni-switch/releases/tag/v99.0.0",
  downloadUrl: null,
  notes: "模型配置优化\n<img src=x onerror=alert(1)>",
  publishedAt: null,
  checkedAt: 1791360000,
};
let client: QueryClient;
beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  vi.spyOn(api, "updateSource").mockResolvedValue({
    currentVersion: APP_VERSION,
    repository: "example/uni-switch",
  });
  vi.spyOn(api, "checkUpdate").mockResolvedValue(release);
  vi.spyOn(api, "openRelease").mockResolvedValue();
});
afterEach(() => {
  client.clear();
  vi.restoreAllMocks();
});
function Panel() {
  return <AppUpdatePanel state={useAppUpdate()} />;
}
function mount() {
  render(
    <QueryClientProvider client={client}>
      <Panel />
    </QueryClientProvider>,
  );
}
describe("GitHub版本更新", () => {
  it("自动检测一次并展示新版本，更新说明按纯文本展示，下载打开本应用发布页", async () => {
    mount();
    await screen.findByText("新版本 v99.0.0");
    expect(api.checkUpdate).toHaveBeenCalledOnce();
    expect(screen.getByText(/<img src=x/)).toBeVisible();
    expect(screen.queryByRole("img")).toBeNull();
    expect(screen.queryByRole("dialog")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "前往下载" }));
    expect(api.openRelease).toHaveBeenCalledExactlyOnceWith(release.releaseUrl);
  });
  it("相同或本地更高版本不显示更新提示，手动检测可刷新", async () => {
    vi.mocked(api.checkUpdate).mockResolvedValue({
      ...release,
      latestVersion: APP_VERSION,
      available: false,
    });
    mount();
    await screen.findByText("已是最新版本");
    expect(screen.queryByText(/新版本 v/)).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "检测更新" }));
    await waitFor(() => expect(api.checkUpdate).toHaveBeenCalledTimes(2));
  });
  it("网络失败提示重试，保留上次成功结果，不误报最新且不重复请求", async () => {
    mount();
    await screen.findByText("新版本 v99.0.0");
    vi.mocked(api.checkUpdate).mockRejectedValue(
      new Error("无法连接 GitHub，请检查网络后重试"),
    );
    await userEvent.click(screen.getByRole("button", { name: "检测更新" }));
    await screen.findByRole("alert");
    expect(screen.getByText("新版本 v99.0.0")).toBeVisible();
    expect(screen.queryByText("已是最新版本")).toBeNull();
    expect(api.checkUpdate).toHaveBeenCalledTimes(2);
  });
  it("检测进行中禁用重复点击，浏览器失败提供可复制的发布页", async () => {
    let resolve!: (value: UpdateCheck) => void;
    vi.mocked(api.checkUpdate).mockImplementation(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    vi.mocked(api.openRelease).mockRejectedValue(new Error("未能打开浏览器"));
    mount();
    const checking = await screen.findByRole("button", { name: "检测中…" });
    expect(checking).toBeDisabled();
    await userEvent.click(checking);
    expect(api.checkUpdate).toHaveBeenCalledOnce();
    resolve(release);
    await userEvent.click(
      await screen.findByRole("button", { name: "前往下载" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "未能打开浏览器",
    );
    expect(screen.getByLabelText("发布页地址")).toHaveValue(release.releaseUrl);
  });
  it("发布仓库未绑定时不请求更新、不声称最新", async () => {
    vi.mocked(api.updateSource).mockResolvedValue({
      currentVersion: APP_VERSION,
      repository: null,
    });
    mount();
    await screen.findByText(/此构建尚未绑定 GitHub/);
    expect(api.checkUpdate).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "检测更新" })).toBeDisabled();
    expect(screen.queryByText("已是最新版本")).toBeNull();
  });
});
