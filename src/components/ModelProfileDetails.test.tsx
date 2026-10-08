import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "../lib/api";
import type { useConnectionDiscovery } from "../lib/useConnectionDiscovery";
import type { ModelVerification, ProviderModel } from "../types";
import { ModelProfileDetails } from "./ModelProfileDetails";

const model: ProviderModel = {
  id: "claude-haiku-5-5",
  enabled: true,
  contextWindow: 256000,
  reasoningEfforts: [],
};
function discovery(baseUrl = "https://example.test/v1", preview = false) {
  return {
    connection: { baseUrl, apiKey: "synthetic-key" },
    protocol: "anthropic",
    authMode: "bearer",
    busy: false,
    preview,
    setThinkingFormat: vi.fn(),
  } as unknown as ReturnType<typeof useConnectionDiscovery>;
}
const observation: ModelVerification = {
  model: model.id,
  endpoint: "messages",
  feature: "text",
  state: "verified",
  checkedAt: 1,
  message: "合成测试成功",
};
afterEach(() => vi.restoreAllMocks());
async function openDetails() {
  await userEvent.click(screen.getByText("接口、思考参数与接入验证"));
  return screen.findByLabelText(`转换请求的思考模式（${model.id}）`);
}
describe("模型档案与显式验证", () => {
  it("折叠时不挂载控件也不发送验证请求，展开展示信息而不修改上下文", async () => {
    const verify = vi.spyOn(api, "verifyModel").mockResolvedValue(observation);
    const state = discovery();
    render(<ModelProfileDetails model={model} discovery={state} />);
    expect(screen.queryByRole("combobox")).toBeNull();
    expect(verify).not.toHaveBeenCalled();
    const select = await openDetails();
    expect(screen.getByText(/仅展示资料，不修改当前上下文设置/)).toBeVisible();
    expect(screen.getByText(/客户端八档菜单保持不变/)).toBeVisible();
    await userEvent.selectOptions(select, "budget");
    expect(state.setThinkingFormat).toHaveBeenCalledWith(model.id, "budget");
    expect(verify).not.toHaveBeenCalled();
  });
  it("未确认计费不能验证，确认后只发送所选接口和项目", async () => {
    const verify = vi.spyOn(api, "verifyModel").mockResolvedValue(observation);
    const state = discovery();
    render(<ModelProfileDetails model={model} discovery={state} />);
    await openDetails();
    const start = screen.getByRole("button", { name: "开始验证" });
    expect(start).toBeDisabled();
    await userEvent.selectOptions(
      screen.getByLabelText("验证接口"),
      "chat_completions",
    );
    await userEvent.selectOptions(screen.getByLabelText("验证项目"), "tools");
    await userEvent.click(screen.getByRole("checkbox"));
    await userEvent.click(start);
    expect(verify).toHaveBeenCalledExactlyOnceWith(
      state.connection,
      model.id,
      "bearer",
      "chat_completions",
      "tools",
      true,
    );
    expect(await screen.findByText("合成测试成功")).toBeVisible();
  });
  it("连接变化清除同意状态并丢弃旧连接的延迟结果", async () => {
    let resolve!: (value: ModelVerification) => void;
    vi.spyOn(api, "verifyModel").mockImplementation(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    const rendered = render(
      <ModelProfileDetails model={model} discovery={discovery()} />,
    );
    await openDetails();
    await userEvent.click(screen.getByRole("checkbox"));
    await userEvent.click(screen.getByRole("button", { name: "开始验证" }));
    rendered.rerender(
      <ModelProfileDetails
        model={model}
        discovery={discovery("https://other.test/v1")}
      />,
    );
    await waitFor(() => expect(screen.getByRole("checkbox")).not.toBeChecked());
    resolve(observation);
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "开始验证" })).toBeDisabled(),
    );
    expect(screen.queryByText("合成测试成功")).toBeNull();
  });
  it("浏览器预览不允许发送真实验证请求", async () => {
    const verify = vi.spyOn(api, "verifyModel").mockResolvedValue(observation);
    render(
      <ModelProfileDetails
        model={model}
        discovery={discovery(undefined, true)}
      />,
    );
    await openDetails();
    expect(screen.getByRole("checkbox")).toBeDisabled();
    expect(screen.getByRole("button", { name: "开始验证" })).toBeDisabled();
    expect(screen.getByText("浏览器预览不发送验证请求。")).toBeVisible();
    expect(verify).not.toHaveBeenCalled();
  });
});
