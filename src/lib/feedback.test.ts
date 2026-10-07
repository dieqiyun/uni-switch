import { describe, expect, it } from "vitest";
import { explainError, safeDiagnostic, providerState } from "./feedback";
import type { RuntimeStatus, TargetStatus } from "../types";
describe("结果与异常提示", () => {
  it("认证、限流、目录冲突给不同操作", () => {
    expect(explainError({ code: "http", message: "HTTP 401" }).field).toBe(
      "apiKey",
    );
    expect(explainError({ code: "http", message: "HTTP 404" }).field).toBe(
      "baseUrl",
    );
    expect(explainError({ code: "http", message: "HTTP 429" }).action).toBe(
      "稍后重试",
    );
    expect(
      explainError({ code: "directory_selection", message: "ambiguous" })
        .action,
    ).toBe("选择配置位置");
  });
  it("诊断不包含输入密钥与Bearer内容", () => {
    const output = safeDiagnostic(
      {
        code: "network",
        message: "fake-private-value Bearer second-secret sk-third-secret",
      },
      ["fake-private-value"],
    );
    expect(output).not.toMatch(
      /fake-private-value|second-secret|sk-third-secret/,
    );
  });
  it("文件成功不能掩盖重启、服务恢复或待更新", () => {
    const status = { state: "applied" } as TargetStatus;
    const runtime = {
      restartRequired: true,
      bridgeRequired: false,
      bridgeHealthy: true,
    } as RuntimeStatus;
    expect(providerState(status, runtime, "codex").label).toBe("待重启");
    expect(
      providerState(
        status,
        { ...runtime, bridgeRequired: true, bridgeHealthy: false },
        "codex",
      ).label,
    ).toBe("正在恢复");
    expect(
      providerState({ ...status, state: "saved_changes" }, runtime, "codex")
        .label,
    ).toBe("待更新");
    expect(providerState(status, undefined, "codex").label).toBe("已配置");
  });
});
