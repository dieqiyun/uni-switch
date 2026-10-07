import { useEffect, useId, useRef, useState } from "react";
import { RefreshCw } from "lucide-react";
import type {
  Provider,
  ProviderModel,
  QuickModelInput,
  Target,
} from "../types";
import {
  providerProtocol,
  modelAllowed,
  useConnectionDiscovery,
} from "../lib/useConnectionDiscovery";
import { contextTokens, DEFAULT_MODEL_CONTEXT } from "../lib/modelContext";
import { ModelPicker } from "./ModelPicker";
import { Modal } from "./Modal";

type Save = (input: QuickModelInput, label: string) => Promise<boolean>;
export function savedModels(provider: Provider): ProviderModel[] {
  const models = (provider.codexOptions?.models || []).map((m) => ({
    ...m,
    contextWindow:
      provider.codexOptions?.contextWindow ??
      m.contextWindow ??
      DEFAULT_MODEL_CONTEXT,
  }));
  if (!models.some((m) => m.id === provider.model))
    models.unshift({
      id: provider.model,
      enabled: true,
      contextWindow: DEFAULT_MODEL_CONTEXT,
      reasoningEfforts: [],
    });
  return models;
}
export function ProviderModelSelect({
  provider,
  target,
  displayedModel,
  disabled,
  expanded,
  onExpand,
  onSave,
}: {
  provider: Provider;
  target: Target;
  displayedModel: string;
  disabled: boolean;
  expanded: boolean;
  onExpand: (event: React.MouseEvent<HTMLButtonElement>) => void;
  onSave: Save;
}) {
  const models = savedModels(provider);
  const context =
    models.find((m) => m.id === displayedModel)?.contextWindow ??
    DEFAULT_MODEL_CONTEXT;
  const [length, setLength] = useState(String(context / 1000));
  const [error, setError] = useState("");
  const skipBlur = useRef(false);
  const pending = useRef(false);
  const id = useId();
  useEffect(() => {
    setLength(String(context / 1000));
    setError("");
  }, [context, displayedModel, provider.updatedAt]);
  async function saveContext() {
    if (skipBlur.current) {
      skipBlur.current = false;
      return;
    }
    if (disabled || pending.current) return;
    const value = contextTokens(length);
    if (value === null) {
      setError("请输入 0.001–100000 之间的 k 值，最多三位小数。");
      return;
    }
    setError("");
    if (value === context) return;
    pending.current = true;
    try {
      const ok = await onSave(
        {
          expected: provider,
          target,
          model: provider.model,
          models: models.map((m) =>
            m.id === displayedModel ? { ...m, contextWindow: value } : m,
          ),
        },
        "上下文已更新",
      );
      if (!ok) setLength(String(context / 1000));
    } finally {
      pending.current = false;
    }
  }
  return (
    <>
      <div className="provider-setting provider-model-setting">
        <span className="provider-setting-label">默认模型</span>
        <button
          type="button"
          className="provider-model-button"
          disabled={disabled}
          data-provider-models={provider.id}
          aria-label={`配置 ${provider.name} 的模型`}
          aria-expanded={expanded}
          aria-haspopup="dialog"
          title={`${displayedModel} · 查看全部模型、设置默认模型和上下文`}
          onClick={onExpand}
        >
          <code>{displayedModel}</code>
          <span className="provider-setting-help provider-model-count">
            已启用{" "}
            {
              models.filter(
                (m) =>
                  m.enabled &&
                  modelAllowed(m.id, target, providerProtocol(provider)),
              ).length
            }{" "}
            个 · <span>管理模型</span>
          </span>
        </button>
      </div>
      {target === "codex" && (
        <div className="provider-setting provider-context-quick">
          <label className="provider-setting-label" htmlFor={id}>
            上下文长度
          </label>
          <span className="provider-context-input">
            <input
              id={id}
              aria-label={`上下文长度 · ${provider.name} · ${displayedModel}`}
              aria-invalid={!!error}
              aria-describedby={`${id}-hint${error ? ` ${id}-error` : ""}`}
              inputMode="decimal"
              value={length}
              disabled={
                disabled ||
                expanded ||
                !models.some((m) => m.id === displayedModel)
              }
              title="单位 k；按 Enter 或移开焦点保存，Escape 取消"
              onChange={(e) => {
                setLength(e.target.value);
                setError("");
              }}
              onBlur={() => void saveContext()}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  e.currentTarget.blur();
                }
                if (e.key === "Escape") {
                  e.preventDefault();
                  skipBlur.current = true;
                  setLength(String(context / 1000));
                  setError("");
                  e.currentTarget.blur();
                }
              }}
            />
            <span aria-hidden>k</span>
          </span>
          <span id={`${id}-hint`} className="provider-setting-help">
            修改后自动保存
          </span>
        </div>
      )}
      {error && (
        <p
          className="field-error provider-quick-error"
          id={`${id}-error`}
          role="alert"
        >
          {error}
        </p>
      )}
    </>
  );
}

export function ProviderModelsDialog({
  provider,
  target,
  disabled,
  applied,
  writing = false,
  saveError = "",
  onClose,
  onSave,
}: {
  provider: Provider;
  target: Target;
  disabled: boolean;
  applied: boolean;
  writing?: boolean;
  saveError?: string;
  onClose: () => void;
  onSave: Save;
}) {
  const [original] = useState(provider);
  const discovery = useConnectionDiscovery(
    { providerId: provider.id, baseUrl: provider.baseUrl, apiKey: null },
    provider.authMode,
    target,
    provider,
    providerProtocol(provider),
  );
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const lock = useRef(false);
  const selectionId = useId();
  const savedCount = savedModels(original).filter(
    (m) => m.enabled && modelAllowed(m.id, target, providerProtocol(original)),
  ).length;
  const selectedCount = discovery.models.filter(
    (m) => m.enabled && discovery.allowed(m.id),
  ).length;
  async function save() {
    if (lock.current || disabled || discovery.busy) return;
    if (!discovery.models.some((m) => m.enabled && m.id === discovery.model)) {
      setError("请至少启用一个适用模型，并设置默认模型。");
      return;
    }
    if (discovery.invalidContexts.length) {
      setError("请修正模型的上下文长度。");
      return;
    }
    if (
      discovery.baseUrl.replace(/\/+$/, "") !==
        provider.baseUrl.replace(/\/+$/, "") ||
      discovery.authMode !== provider.authMode ||
      discovery.protocol !== providerProtocol(provider)
    ) {
      setError("供应商返回了不同的接入方式，请进入编辑确认连接信息后再保存。");
      return;
    }
    lock.current = true;
    setSaving(true);
    setError("");
    try {
      const ok = await onSave(
        {
          expected: original,
          target,
          model: discovery.model,
          models: discovery.models,
          syncedAt: discovery.syncedAt,
        },
        "模型配置已保存",
      );
      if (ok) onClose();
      else setError("未能保存。你的选择仍保留，可重试或取消。");
    } finally {
      lock.current = false;
      setSaving(false);
    }
  }
  return (
    <Modal
      title={`${provider.name} · 模型配置`}
      description={
        applied
          ? "保存后应用到当前客户端；取消不修改现有配置。"
          : "保存到此供应商，下次点击使用时生效。"
      }
      className="model-config-dialog"
      wide
      busy={writing || saving}
      dismissOnOutside={false}
      initialFocusId={selectionId}
      onClose={onClose}
    >
      <div className="model-config-body">
        <fieldset disabled={disabled || saving} className="model-config-fields">
          <ModelPicker
            discovery={discovery}
            target={target}
            error={
              error && saveError
                ? `${saveError}。你的选择仍保留，可重试或取消。`
                : error
            }
            selectionId={selectionId}
            title="本次选择"
            draft
            converted={
              target === "claude_desktop" &&
              providerProtocol(provider) === "openai"
            }
          />
          {target === "codex" && (
            <p className="automatic-reasoning-hint">
              每次写入 Codex 配置时自动检查并补齐思考强度选项，无需手动修复。
            </p>
          )}
        </fieldset>
      </div>
      <div className="model-config-actions">
        <span className="model-config-summary" role="status">
          当前已保存 {savedCount} 个，保存后启用 {selectedCount} 个
        </span>
        <button
          type="button"
          className="button secondary"
          disabled={writing || saving}
          onClick={onClose}
        >
          取消
        </button>
        <button
          type="button"
          className="button primary"
          disabled={disabled || saving || discovery.busy}
          onClick={() => void save()}
        >
          {saving && <RefreshCw size={14} className="spinning" aria-hidden />}
          {saving ? "保存中…" : applied ? "保存并应用" : "保存模型配置"}
        </button>
      </div>
    </Modal>
  );
}
