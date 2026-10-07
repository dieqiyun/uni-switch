import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { RefreshCw } from "lucide-react";
import { api, desktopRuntime, errorMessage } from "../lib/api";
import {
  providerProtocol,
  needsProtocolConversion,
  protocolConfirmed,
  protocolConversionEnabled,
} from "../lib/protocolConversion";
import type { Overview, Provider, Target } from "../types";

export function ProviderProtocolControl({
  provider,
  target,
  active,
  disabled,
  toggleDisabled,
  saving,
  onToggle,
}: {
  provider: Provider;
  target: Target;
  active: boolean;
  disabled: boolean;
  toggleDisabled?: boolean;
  saving: boolean;
  onToggle: (provider: Provider, enabled: boolean) => void;
}) {
  const client = useQueryClient();
  const confirmed = protocolConfirmed(provider);
  const detection = useQuery({
    queryKey: ["provider-protocol", provider],
    queryFn: () => api.detectProtocol(provider),
    enabled: desktopRuntime && !confirmed && !disabled,
    staleTime: Infinity,
    retry: false,
    refetchOnWindowFocus: false,
  });
  useEffect(() => {
    if (!detection.data) return;
    client.setQueryData<Overview>(
      ["overview"],
      (current) =>
        current && {
          ...current,
          providers: current.providers.map((p) =>
            JSON.stringify(p) === JSON.stringify(provider)
              ? detection.data!
              : p,
          ),
        },
    );
    void client.invalidateQueries({ queryKey: ["overview"] });
  }, [detection.data, client, provider]);
  const name = providerProtocol(provider) === "openai" ? "OpenAI" : "Claude";
  const conversion = needsProtocolConversion(provider, target);
  const enabled = protocolConversionEnabled(provider, target);
  const destination = target === "codex" ? "OpenAI" : "Claude";
  const hintId = `protocol-hint-${provider.id}`;
  return (
    <div className="provider-protocol-row">
      <span className="provider-protocol-label">API 协议</span>
      <span className="provider-protocol-value">
        {confirmed || !desktopRuntime
          ? name
          : detection.isFetching
            ? "检测中…"
            : "未确认"}
        <span className="provider-protocol-source">
          {provider.codexOptions?.protocolPreference
            ? "手动指定"
            : confirmed || desktopRuntime
              ? "自动检测"
              : "待确认"}
        </span>
      </span>
      {conversion && (confirmed || !desktopRuntime) ? (
        <>
          <label className="provider-fast-toggle provider-conversion-toggle">
            <input
              type="checkbox"
              role="switch"
              aria-label={`转换为 ${destination} · ${provider.name}`}
              aria-describedby={hintId}
              checked={enabled}
              disabled={
                disabled || toggleDisabled || detection.isFetching || saving
              }
              onChange={(event) => onToggle(provider, event.target.checked)}
            />
            <span className="provider-fast-track" aria-hidden />
            <span>{saving ? "保存中…" : `转换为 ${destination}`}</span>
          </label>
          <span id={hintId} className="provider-protocol-hint">
            {toggleDisabled
              ? "请先处理当前配置，再修改转换开关"
              : active && enabled
                ? "已启用；关闭会停用并恢复原配置"
                : enabled
                  ? "已启用 · 使用时自动转换"
                  : "已关闭 · 开启后可使用"}
          </span>
        </>
      ) : (
        <span id={hintId} className="provider-protocol-hint">
          {confirmed ? "直接接入，无需转换" : "检测后自动匹配接入方式"}
        </span>
      )}
      {!!detection.error && !confirmed && (
        <span className="provider-protocol-error" role="status">
          <span title={errorMessage(detection.error)}>协议检测失败</span>
          <button
            type="button"
            className="text-button"
            disabled={detection.isFetching || disabled}
            onClick={() => void detection.refetch()}
          >
            <RefreshCw size={12} aria-hidden />
            重新检测
          </button>
        </span>
      )}
    </div>
  );
}
