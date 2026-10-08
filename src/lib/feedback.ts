import { errorMessage } from "./api";
import { APP_VERSION } from "./appVersion";
import type { Provider, RuntimeStatus, Target, TargetStatus } from "../types";

export function errorCode(error: unknown) {
  return error && typeof error === "object" && "code" in error
    ? String(error.code)
    : "";
}
export function explainError(error: unknown) {
  const raw = errorMessage(error);
  const code = errorCode(error);
  if (/429/.test(raw))
    return {
      message: "供应商暂时限制请求，请稍后重试。填写内容已保留。",
      action: "稍后重试",
      field: null,
    };
  if (/401/.test(raw) || code === "missing_key")
    return {
      message: "密钥未通过验证，请检查是否复制完整或已经失效。",
      action: "修改密钥",
      field: "apiKey" as const,
    };
  if (/403/.test(raw))
    return {
      message: "此密钥没有访问权限，请核对供应商的模型权限或 IP 限制。",
      action: "检查密钥",
      field: "apiKey" as const,
    };
  if (/404|405/.test(raw) || code === "invalid_url" || code === "supplier_json")
    return {
      message: "未找到可用的模型接口，请核对供应商提供的 API 接入地址。",
      action: "修改地址",
      field: "baseUrl" as const,
    };
  if (code === "directory_selection" || code === "discovery_changed")
    return {
      message: "配置位置尚未确定，请在下方选择客户端实际使用的位置。",
      action: "选择配置位置",
      field: null,
    };
  if (code === "external_change")
    return {
      message:
        "其他软件修改了 API 配置，本次没有覆盖。点击「使用」可确认覆盖，也可先查看配置。",
      action: "查看配置",
      field: null,
    };
  if (code === "provider_changed" || code === "configuration_changed")
    return { message: raw, action: "刷新配置", field: null };
  if (code === "bridge_unavailable")
    return {
      message: "后台兼容服务正在恢复，原配置已保留。恢复后可重试。",
      action: "重试",
      field: null,
    };
  if (
    code === "no_models" ||
    code === "model_response" ||
    /没有返回可用模型|未找到.*模型/.test(raw)
  )
    return {
      message:
        "此密钥没有返回可用模型。请检查模型权限；已有供应商可继续使用已保存的模型。",
      action: "重新获取",
      field: null,
    };
  if (code === "network" || /超时|网络|连接.*失败/.test(raw))
    return {
      message: "暂时无法连接供应商。填写内容已保留，可以检查网络后重试。",
      action: "重试",
      field: null,
    };
  return { message: raw, action: "重试", field: null };
}
export function safeDiagnostic(error: unknown, secrets: string[] = []) {
  let message = errorMessage(error);
  for (const secret of secrets
    .filter(Boolean)
    .sort((a, b) => b.length - a.length))
    message = message.split(secret).join("[已隐藏]");
  message = message.replace(/\b(?:sk-[\w-]+|Bearer\s+[^\s,;]+)/gi, "[已隐藏]");
  return `uni-switch ${APP_VERSION}\n错误类型：${errorCode(error) || "unknown"}\n${message}`;
}
export function providerState(
  status: TargetStatus | undefined,
  runtime: RuntimeStatus | undefined,
  target: Target,
) {
  if (status?.state === "external_change" || status?.state === "error")
    return { label: "配置需处理", note: status.message, tone: "attention" };
  if (status?.state === "saved_changes")
    return {
      label: "待更新",
      note: "供应商已修改，更新后用于此客户端。",
      tone: "attention",
    };
  if (runtime?.bridgeRequired && !runtime.bridgeHealthy)
    return {
      label: "正在恢复",
      note: "后台兼容服务正在自动恢复，配置已保留。",
      tone: "attention",
    };
  if (runtime?.restartRequired)
    return {
      label: "待重启",
      note: runtime.restartInProgress
        ? "接续终端已打开，请在原 Claude CLI 输入 /exit，退出后自动恢复会话。"
        : target === "claude_cli"
          ? "请退出旧 CLI 进程并重新启动，加载本次配置。"
          : target === "codex"
            ? "桌面端请重启；CLI 请退出旧进程并重新启动。"
            : "请重启 Claude 桌面客户端，加载本次配置。",
      tone: "attention",
    };
  return {
    label: runtime ? "已应用" : "已配置",
    note: "配置文件已校验。新会话使用此配置。",
    tone: "ready",
  };
}
export function displayProviderName(provider: Provider, providers: Provider[]) {
  if (
    providers.some(
      (p) =>
        p.id !== provider.id &&
        p.name === provider.name &&
        p.keySuffix === provider.keySuffix,
    )
  ) {
    try {
      return `${provider.name} · ${new URL(provider.baseUrl).pathname.replace(/\/+$/, "") || "根地址"}`;
    } catch {
      /* URL validated before save. */
    }
  }
  return providers.some((p) => p.id !== provider.id && p.name === provider.name)
    ? `${provider.name} · …${provider.keySuffix || "未标记"}`
    : provider.name;
}
export function providerMatches(provider: Provider, search: string) {
  return [
    provider.name,
    provider.baseUrl,
    provider.model,
    provider.keySuffix,
    ...(provider.codexOptions?.models
      .filter((m) => m.enabled)
      .map((m) => m.id) || []),
  ]
    .join(" ")
    .toLowerCase()
    .includes(search.trim().toLowerCase());
}
