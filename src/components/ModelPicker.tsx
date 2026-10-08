import { useEffect, useId, useState } from "react";
import { RefreshCw, Search, Check, X } from "lucide-react";
import type { Target } from "../types";
import { capabilityFields, modelCapability } from "../lib/modelCapabilities";
import type { useConnectionDiscovery } from "../lib/useConnectionDiscovery";
import { ModelProfileDetails } from "./ModelProfileDetails";

export function ModelPicker({
  discovery,
  target,
  error,
  autoWrite = false,
  converted = false,
  selectionId = "model-selection",
  title = "启用模型",
  draft = false,
}: {
  discovery: ReturnType<typeof useConnectionDiscovery>;
  target: Target;
  error: string;
  autoWrite?: boolean;
  converted?: boolean;
  selectionId?: string;
  title?: string;
  draft?: boolean;
}) {
  const [search, setSearch] = useState("");
  const [showUnavailable, setShowUnavailable] = useState(false);
  const [onlySelected, setOnlySelected] = useState(false);
  const [limit, setLimit] = useState(40);
  useEffect(() => setLimit(40), [search, onlySelected, showUnavailable]);
  const fieldPrefix = useId();
  useEffect(() => {
    if (error) {
      setSearch("");
      setOnlySelected(false);
      const section = document.getElementById(selectionId);
      section?.focus();
      section?.scrollIntoView?.({ block: "nearest" });
    }
  }, [error, selectionId]);
  const visible = discovery.models
    .filter(
      (m) =>
        (showUnavailable || m.enabled || discovery.allowed(m.id)) &&
        (!onlySelected ||
          m.enabled ||
          discovery.invalidContexts.includes(m.id)) &&
        m.id.toLowerCase().includes(search.trim().toLowerCase()),
    )
    .sort(
      (a, b) =>
        Number(b.id === discovery.model) - Number(a.id === discovery.model) ||
        Number(discovery.invalidContexts.includes(b.id)) -
          Number(discovery.invalidContexts.includes(a.id)) ||
        Number(b.enabled) - Number(a.enabled),
    );
  const selected = discovery.models.filter(
    (m) => m.enabled && discovery.allowed(m.id),
  ).length;
  const available = discovery.models.filter((m) =>
    discovery.allowed(m.id),
  ).length;
  return (
    <section
      className="model-selection"
      id={selectionId}
      tabIndex={-1}
      aria-labelledby={`${selectionId}-title`}
      aria-busy={discovery.busy}
    >
      <div className="model-selection-heading">
        <h3 id={`${selectionId}-title`}>{title}</h3>
        <span className="auto-balance-label">自动同步</span>
        {discovery.models.length > 0 && (
          <span className="model-selection-count">
            {draft ? "已选择" : "已启用"} {selected} / {available}
          </span>
        )}
        {discovery.ready && !discovery.preview && (
          <button
            type="button"
            className="balance-refresh"
            disabled={discovery.busy}
            onClick={() => void discovery.refresh()}
            aria-label="刷新模型列表"
          >
            <RefreshCw
              size={14}
              aria-hidden
              className={discovery.busy ? "spinning" : ""}
            />
            {discovery.busy ? "同步中" : discovery.failure ? "重试" : "刷新"}
          </button>
        )}
      </div>
      <p
        id={`${selectionId}-hint`}
        className="model-selection-hint"
        role="status"
      >
        {discovery.models.length
          ? target === "codex"
            ? "上游可用模型默认全选，可按需取消。默认模型用于启动，其他已启用模型可在 Codex 中选择。"
            : target === "claude_cli"
              ? "上游可用模型默认全选，可按需取消。Claude CLI 启动时使用默认模型。"
              : "上游可用模型默认全选，可按需取消。默认模型排在 Claude 桌面端列表首位。"
          : discovery.preview
            ? "在桌面端填写地址和密钥后，自动获取模型列表。"
            : discovery.busy
              ? "正在获取这把密钥可用的模型…"
              : discovery.ready
                ? "即将自动同步模型列表…"
                : "填写 API 地址和密钥后，自动获取模型列表。"}
      </p>
      {discovery.models.some((m) => !discovery.allowed(m.id)) && (
        <button
          type="button"
          className="text-button model-unavailable-toggle"
          aria-pressed={showUnavailable}
          onClick={() => setShowUnavailable((v) => !v)}
        >
          {showUnavailable ? "隐藏不适用模型" : "查看不适用模型"}
        </button>
      )}
      {discovery.models.length > 8 && (
        <div className="model-search input-with-action">
          <Search size={15} aria-hidden />
          <input
            aria-label="搜索模型"
            placeholder="搜索模型"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Escape" && search) {
                e.preventDefault();
                e.stopPropagation();
                setSearch("");
              }
            }}
          />
          {search && (
            <button
              type="button"
              className="icon-button"
              aria-label="清空模型搜索"
              onClick={() => setSearch("")}
            >
              <X size={15} aria-hidden />
            </button>
          )}
        </div>
      )}
      {!!discovery.models.length && (
        <div className="model-filter-bar">
          <button
            type="button"
            className="text-button"
            aria-pressed={onlySelected}
            onClick={() => {
              setOnlySelected((v) => !v);
              setSearch("");
            }}
          >
            {onlySelected
              ? "查看全部模型"
              : `只看${draft ? "已选择" : "已启用"}（${selected}）`}
          </button>
          <span>
            {visible.length} 个{search.trim() ? "匹配" : "可见"}模型
          </span>
        </div>
      )}
      {discovery.models.length > 0 && (
        <div
          className="model-picks"
          role="group"
          aria-label="可启用的模型"
          aria-describedby={`${selectionId}-hint${error ? ` ${selectionId}-error` : ""}`}
          aria-invalid={!!error}
        >
          {visible.slice(0, limit).map((m) => (
            <div
              className={`model-choice ${m.enabled ? "is-enabled" : ""}`}
              key={m.id}
            >
              <label className="model-choice-label">
                <input
                  type="checkbox"
                  checked={m.enabled}
                  disabled={!discovery.allowed(m.id)}
                  aria-label={`启用 ${m.id}`}
                  onChange={(e) => discovery.toggle(m.id, e.target.checked)}
                />
                <code>{m.id}</code>
              </label>
              {target === "codex" && (
                <div className="model-context">
                  <label htmlFor={`${fieldPrefix}-${m.id}`}>上下文</label>
                  <span className="model-context-input">
                    <input
                      id={`${fieldPrefix}-${m.id}`}
                      aria-label={`上下文长度 ${m.id}`}
                      aria-describedby={`${selectionId}-context-hint${discovery.invalidContexts.includes(m.id) ? ` ${fieldPrefix}-${m.id}-error` : ""}`}
                      aria-invalid={discovery.invalidContexts.includes(m.id)}
                      inputMode="decimal"
                      disabled={!m.enabled || !discovery.allowed(m.id)}
                      value={discovery.contexts[m.id] ?? "256"}
                      onChange={(e) =>
                        discovery.setContext(m.id, e.target.value)
                      }
                    />
                    <span aria-hidden>k</span>
                  </span>
                </div>
              )}
              <div className="model-choice-action">
                {!discovery.allowed(m.id) ? (
                  <span className="model-choice-note">不适用</span>
                ) : m.id === discovery.model ? (
                  <span className="model-default">
                    <Check size={12} aria-hidden />
                    默认
                  </span>
                ) : (
                  <button
                    type="button"
                    className="model-default-button"
                    aria-label={`将 ${m.id} 设为默认模型`}
                    onClick={() => discovery.selectDefault(m.id)}
                    title={
                      m.enabled
                        ? "作为启动时默认模型"
                        : "自动启用此模型并设为默认"
                    }
                  >
                    {m.enabled ? "设为默认" : "启用并设为默认"}
                  </button>
                )}
              </div>
              <div
                className="model-capabilities"
                role="group"
                aria-label={`模型能力 ${m.id}`}
              >
                {capabilityFields
                  .filter(
                    (field) => target === "codex" || field.key === "imageInput",
                  )
                  .map((field) => {
                    const capability = modelCapability(m, field.key);
                    const sourceLabel = {
                      manual: "手动",
                      upstream: "上游返回",
                      official: "自动匹配",
                      unknown: "待确认",
                    }[capability.source];
                    return (
                      <label
                        key={field.key}
                        className="model-capability"
                        title={field.help}
                      >
                        <input
                          type="checkbox"
                          checked={capability.value}
                          disabled={!discovery.allowed(m.id)}
                          aria-label={`${field.label} ${m.id}`}
                          onChange={(e) =>
                            discovery.setCapability(
                              m.id,
                              field.key,
                              e.target.checked,
                            )
                          }
                        />
                        <span>{field.label}</span>
                        <span
                          className={`capability-source ${capability.source === "unknown" ? "is-unknown" : ""}`}
                        >
                          {sourceLabel}
                        </span>
                      </label>
                    );
                  })}
                {(m.profileOverrides?.thinkingFormat != null ||
                  Object.values(m.capabilityOverrides ?? {}).some(
                    (v) => typeof v === "boolean",
                  )) && (
                  <button
                    type="button"
                    className="text-button capability-reset"
                    aria-label={`恢复自动能力 ${m.id}`}
                    onClick={() => discovery.resetCapabilities(m.id)}
                  >
                    恢复自动
                  </button>
                )}
                {modelCapability(m, "imageInput").source === "unknown" && (
                  <span className="capability-unknown-hint">
                    未识别图片能力，请按上游说明勾选。
                  </span>
                )}
              </div>
              {discovery.invalidContexts.includes(m.id) && (
                <p
                  className="field-error model-context-error"
                  id={`${fieldPrefix}-${m.id}-error`}
                  role="alert"
                >
                  请输入 0.001–100000 之间的 k 值，最多三位小数
                </p>
              )}
              <ModelProfileDetails model={m} discovery={discovery} />
            </div>
          ))}
          {!visible.length && (
            <div className="model-empty">
              <p className="model-selection-hint">
                {onlySelected && !selected
                  ? "还没有启用模型，请从全部模型中选择。"
                  : "没有匹配的模型"}
              </p>
              <button
                type="button"
                className="text-button"
                onClick={() => {
                  setSearch("");
                  setOnlySelected(false);
                  setShowUnavailable(false);
                }}
              >
                查看可用模型
              </button>
            </div>
          )}
        </div>
      )}
      {visible.length > limit && (
        <div className="model-load-more">
          <span>
            已显示 {limit} / {visible.length} 个，搜索可直接定位。
          </span>
          <button
            type="button"
            className="button secondary"
            onClick={() => setLimit((v) => v + 40)}
          >
            显示更多模型
          </button>
        </div>
      )}
      {!!discovery.models.length && (
        <p className="model-selection-footnote">
          能力优先采用手动设置，其次为上游返回，再按官方资料匹配。刷新会保留手动设置。图片输入指识图，不是生成图片。
          {target !== "codex" &&
            "图片能力会在协议转换服务中生效；Claude 原生模型菜单由客户端管理。"}
        </p>
      )}
      {target === "codex" && !!discovery.models.length && (
        <p
          id={`${selectionId}-context-hint`}
          className="model-selection-footnote"
        >
          每个模型默认 256k，1k = 1,000 Token。压缩设置随上下文长度一起更新。
          实际支持长度以供应商为准。
        </p>
      )}
      {target === "claude_desktop" &&
        converted &&
        !!discovery.models.length && (
          <p className="model-selection-footnote">
            确认使用后，所选 GPT 模型会写入 Claude 桌面配置，模型菜单显示真实
            GPT 名称。重启 Claude 后即可选择。
          </p>
        )}
      {discovery.busy && !!discovery.models.length && (
        <p className="model-selection-hint" role="status">
          正在更新模型列表…
        </p>
      )}
      {discovery.failure && (
        <p className="balance-failure" role="status">
          {discovery.failure}
          {discovery.models.some((m) => m.enabled)
            ? "。可继续使用已保存的模型。"
            : "。检查地址和密钥后可重试。"}
        </p>
      )}
      {!discovery.failure &&
        target === "codex" &&
        !!discovery.models.length && (
          <p className="model-selection-footnote">
            {autoWrite
              ? "已启用的模型会自动写入当前 Codex 配置，重启后即可在模型菜单中选择。"
              : "确认使用时会自动写入启用的模型，重启 Codex 后即可在模型菜单中选择。"}
          </p>
        )}
      {error && (
        <p className="field-error" id={`${selectionId}-error`} role="alert">
          {error}
        </p>
      )}
    </section>
  );
}
