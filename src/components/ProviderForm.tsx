import { useEffect, useRef, useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import {
  Eye,
  EyeOff,
  ArrowRight,
  SlidersHorizontal,
  Check,
  RefreshCw,
} from "lucide-react";
import { Modal } from "./Modal";
import { AutoBalance } from "./AutoBalance";
import { ModelPicker } from "./ModelPicker";
import { ErrorFeedback } from "./ErrorFeedback";
import { errorCode, explainError } from "../lib/feedback";
import {
  useConnectionDiscovery,
  providerProtocol,
} from "../lib/useConnectionDiscovery";
import { contextTokens } from "../lib/modelContext";
import { normalizeApiAddress, normalizeApiKey } from "../lib/connectionInput";
import { providerSchema, type FormValues, type FormInput } from "../lib/schema";
import {
  targetNames,
  automaticBalanceQuery,
  type Family,
  type Provider,
  type ProviderInput,
  type Target,
} from "../types";

export function ProviderForm({
  family,
  target,
  provider,
  applyOnSave = true,
  onCommit,
  onComplete,
  onClose,
  startWithModels = false,
}: {
  family: Family;
  target: Target;
  provider?: Provider;
  applyOnSave?: boolean;
  onCommit: (input: ProviderInput, apply: boolean) => Promise<Provider>;
  onComplete: (applied: boolean) => void;
  onClose: () => void;
  startWithModels?: boolean;
}) {
  const [showKey, setShowKey] = useState(false);
  const [failure, setFailure] = useState<unknown>(null);
  const [inputNotice, setInputNotice] = useState("");
  const [phase, setPhase] = useState<"idle" | "models" | "writing">("idle");
  const [slow, setSlow] = useState(false);
  const submitGeneration = useRef(0);
  const formRef = useRef<HTMLFormElement>(null);
  useEffect(
    () => () => {
      submitGeneration.current++;
    },
    [],
  );
  useEffect(() => {
    setSlow(false);
    if (phase !== "models") return;
    const timer = setTimeout(() => setSlow(true), 6000);
    return () => clearTimeout(timer);
  }, [phase]);
  const close = () => {
    if (phase === "writing") return;
    submitGeneration.current++;
    onClose();
  };
  const [modelError, setModelError] = useState("");
  const [saveOnly, setSaveOnly] = useState(false);
  const {
    register,
    handleSubmit,
    setError,
    setFocus,
    setValue,
    watch,
    formState: { errors, isSubmitting },
  } = useForm<FormInput, unknown, FormValues>({
    resolver: zodResolver(providerSchema),
    defaultValues: {
      name: provider?.name || "我的 API",
      baseUrl: provider?.baseUrl || "",
      apiKey: "",
      protocol: provider
        ? provider.codexOptions?.upstreamProtocol
          ? provider.codexOptions.protocolPreference || "auto"
          : providerProtocol(provider)
        : "auto",
      authMode: provider
        ? provider.codexOptions?.upstreamProtocol
          ? provider.codexOptions.authPreference || "auto"
          : provider.authMode
        : "auto",
      fastMode:
        provider?.codexOptions?.fastMode == null
          ? ""
          : provider.codexOptions.fastMode
            ? "on"
            : "off",
    },
  });
  const input = {
    providerId: provider?.id || null,
    baseUrl: normalizeApiAddress(watch("baseUrl")),
    apiKey: normalizeApiKey(watch("apiKey")) || null,
  };
  const discovery = useConnectionDiscovery(
    input,
    watch("authMode") || "auto",
    target,
    provider,
    watch("protocol") || "auto",
  );
  const converted =
    target === "codex"
      ? discovery.protocol === "anthropic"
      : discovery.protocol === "openai";
  const preservedBalance =
    !input.apiKey &&
    input.baseUrl.replace(/\/+$/, "") === provider?.baseUrl.replace(/\/+$/, "")
      ? provider?.codexOptions?.balanceQuery
      : null;
  useEffect(() => setModelError(""), [discovery.model]);
  const fieldError = (field: keyof FormValues) =>
    errors[field] ? (
      <span id={`${field}-error`} className="field-error" role="alert">
        {errors[field]?.message}
      </span>
    ) : null;
  const accessibility = (field: keyof FormValues) => ({
    "aria-invalid": !!errors[field],
    "aria-describedby": `${field}-hint${errors[field] ? ` ${field}-error` : ""}`,
  });
  const apply = applyOnSave && !saveOnly;
  function cleanField(field: "baseUrl" | "apiKey", value: string) {
    const normalized =
      field === "baseUrl" ? normalizeApiAddress(value) : normalizeApiKey(value);
    if (normalized !== value) {
      setValue(field, normalized, { shouldDirty: true, shouldValidate: true });
      setInputNotice(
        field === "baseUrl"
          ? "已整理为 API 接入地址，保留供应商的站点与路径。"
          : "已去除密钥外侧空白、引号和 Bearer 前缀。",
      );
    }
  }
  const submitLabel = provider
    ? apply
      ? "保存并使用"
      : "保存修改"
    : apply
      ? "添加并使用"
      : "保存供应商";
  return (
    <Modal
      wide
      title={provider ? "编辑供应商" : "添加供应商"}
      description={
        provider
          ? "确认保存后才会更新。密钥留空保留原值。"
          : "填写地址和密钥，自动匹配连接与默认模型。"
      }
      onClose={close}
      busy={phase === "writing"}
      dismissOnOutside={false}
      initialFocusId={startWithModels ? "model-selection" : undefined}
    >
      <div className="editor-destination">
        {apply ? "用于" : "保存到共享供应商列表"}
        <strong>{apply ? targetNames[target] : "Codex / Claude Code"}</strong>
      </div>
      <form
        ref={formRef}
        noValidate
        aria-busy={isSubmitting}
        onSubmit={handleSubmit(async (values) => {
          const request = ++submitGeneration.current;
          const active = () => request === submitGeneration.current;
          setFailure(null);
          setModelError("");
          try {
            if (!provider && !values.apiKey.trim()) {
              setError(
                "apiKey",
                { message: "请填写 API Key" },
                { shouldFocus: true },
              );
              return;
            }
            if (discovery.models.length && !discovery.model) {
              setModelError("请至少启用一个模型");
              return;
            }
            setPhase("models");
            const prepared = await discovery.ensureReady();
            if (!active()) return;
            if (!prepared?.model) {
              setFailure(
                discovery.getFailure() || {
                  code: "no_models",
                  message: "未找到可用模型",
                },
              );
              return;
            }
            if (
              target === "codex" &&
              prepared.models.some(
                (m) =>
                  m.enabled &&
                  contextTokens(prepared.contexts[m.id] ?? "256") === null,
              )
            ) {
              setModelError("请修正模型的上下文长度");
              return;
            }
            const options = {
              ...provider?.codexOptions,
              upstreamProtocol: prepared.protocol,
              protocolDetectedAt: prepared.syncedAt,
              protocol: prepared.protocol,
              claudeProtocol: prepared.protocol,
              protocolPreference:
                values.protocol === "auto" ? null : values.protocol,
              authPreference:
                values.authMode === "auto" ? null : values.authMode,
              fastMode:
                prepared.protocol === "anthropic"
                  ? null
                  : values.fastMode === ""
                    ? null
                    : values.fastMode === "on",
              contextWindow: null,
              autoCompactTokenLimit: null,
              models: prepared.models,
              modelsSyncedAt: prepared.syncedAt,
              balanceQuery: preservedBalance || automaticBalanceQuery,
            };
            setPhase("writing");
            let name = values.name;
            if (!provider && name === "我的 API")
              name = new URL(values.baseUrl).hostname
                .replace(/^api\./, "")
                .slice(0, 80);
            await onCommit(
              {
                id: provider?.id || null,
                family: provider?.family || family,
                name,
                baseUrl: prepared.baseUrl || values.baseUrl,
                apiKey: values.apiKey.trim() || null,
                model: prepared.model,
                authMode: prepared.authMode,
                balanceAccessToken: null,
                reasoningEffort: provider?.reasoningEffort || null,
                codexOptions: options,
              },
              apply,
            );
            onComplete(apply);
            onClose();
          } catch (error) {
            if (!active()) return;
            setFailure(error);
          } finally {
            if (active()) setPhase("idle");
          }
        })}
      >
        <div className="form-fields">
          <fieldset disabled={isSubmitting}>
            <div className="field">
              <label htmlFor="baseUrl">
                API 地址<span aria-hidden>*</span>
              </label>
              <input
                id="baseUrl"
                required
                autoFocus={!startWithModels}
                spellCheck={false}
                placeholder="https://api.example.com/v1"
                {...register("baseUrl", {
                  onBlur: (e) => cleanField("baseUrl", e.target.value),
                })}
                {...accessibility("baseUrl")}
              />
              <small id="baseUrl-hint">粘贴供应商提供的接入地址。</small>
              {fieldError("baseUrl")}
            </div>
            <div className="field">
              <label htmlFor="apiKey">
                API Key<span aria-hidden>{!provider ? "*" : ""}</span>
              </label>
              <div className="input-with-action">
                <input
                  id="apiKey"
                  required={!provider}
                  type={showKey ? "text" : "password"}
                  autoComplete="new-password"
                  spellCheck={false}
                  placeholder={
                    provider ? "留空保留已保存的密钥" : "粘贴供应商密钥"
                  }
                  {...register("apiKey", {
                    onBlur: (e) => cleanField("apiKey", e.target.value),
                  })}
                  {...accessibility("apiKey")}
                />
                <button
                  type="button"
                  className="icon-button"
                  aria-label={showKey ? "隐藏 API Key" : "显示 API Key"}
                  aria-pressed={showKey}
                  onClick={() => setShowKey((v) => !v)}
                >
                  {showKey ? (
                    <EyeOff size={17} aria-hidden />
                  ) : (
                    <Eye size={17} aria-hidden />
                  )}
                </button>
              </div>
              <small id="apiKey-hint">
                {provider
                  ? `密钥已保存${provider.keySuffix ? ` · 尾号 ${provider.keySuffix}` : ""}，填入新值才替换。`
                  : "密钥仅保存在本机。"}
              </small>
              {fieldError("apiKey")}
            </div>
            {inputNotice && (
              <p className="input-cleanup-note" role="status">
                {inputNotice}
              </p>
            )}
            <div
              className={`connection-summary ${discovery.failure ? "has-error" : ""}`}
              aria-busy={discovery.busy}
            >
              <span className="connection-summary-mark" aria-hidden>
                {discovery.busy ? (
                  <RefreshCw size={16} className="spinning" />
                ) : discovery.model ? (
                  <Check size={16} />
                ) : (
                  <SlidersHorizontal size={16} />
                )}
              </span>
              <div role="status">
                <strong>
                  {discovery.model
                    ? `默认使用 ${discovery.model}`
                    : discovery.busy
                      ? "正在匹配供应商…"
                      : "连接与模型自动匹配"}
                </strong>
                <p>
                  {(discovery.failureError
                    ? explainError(discovery.failureError).message
                    : discovery.failure) ||
                    (discovery.model
                      ? `已找到 ${discovery.models.filter((m) => discovery.allowed(m.id)).length} 个可选模型${converted ? "，自动处理客户端兼容" : ""}。`
                      : "填好后直接点击下方按钮即可。")}
                </p>
              </div>
              {discovery.failure && (
                <button
                  type="button"
                  className="text-button"
                  onClick={() => void discovery.refresh()}
                  disabled={discovery.busy}
                >
                  重新匹配
                </button>
              )}
            </div>
            <div className="model-settings">
              <ModelPicker
                title="模型设置"
                discovery={discovery}
                target={target}
                error={modelError}
                converted={converted && target !== "codex"}
              />
            </div>
            <section className="form-advanced" aria-labelledby="advanced-title">
              <h3 className="form-advanced-heading" id="advanced-title">
                <SlidersHorizontal size={15} aria-hidden />
                高级设置<span>名称、协议与其他选项</span>
              </h3>
              <div className="field">
                <label htmlFor="name">供应商名称</label>
                <input
                  id="name"
                  {...register("name")}
                  {...accessibility("name")}
                />
                <small id="name-hint">
                  默认根据地址命名，两个客户端共用这份供应商。
                </small>
                {fieldError("name")}
              </div>
              <div className="advanced-pair">
                <fieldset
                  className="choice-field"
                  aria-describedby="protocol-hint"
                >
                  <legend>API 协议</legend>
                  <div className="choice-options">
                    <label>
                      <input
                        type="radio"
                        value="auto"
                        {...register("protocol")}
                      />
                      自动匹配
                    </label>
                    <label>
                      <input
                        type="radio"
                        value="openai"
                        {...register("protocol")}
                      />
                      OpenAI
                    </label>
                    <label>
                      <input
                        type="radio"
                        value="anthropic"
                        {...register("protocol")}
                      />
                      Claude Messages
                    </label>
                  </div>
                  <small id="protocol-hint">
                    特殊网关可指定。当前匹配：
                    {discovery.protocol === "openai"
                      ? "OpenAI"
                      : "Claude Messages"}
                    。
                  </small>
                </fieldset>
                <fieldset
                  className="choice-field"
                  aria-describedby="authMode-hint"
                >
                  <legend>认证方式</legend>
                  <div className="choice-options">
                    <label>
                      <input
                        type="radio"
                        value="auto"
                        {...register("authMode")}
                      />
                      自动匹配
                    </label>
                    <label>
                      <input
                        type="radio"
                        value="bearer"
                        {...register("authMode")}
                      />
                      Bearer Token
                    </label>
                    <label>
                      <input
                        type="radio"
                        value="x-api-key"
                        {...register("authMode")}
                      />
                      x-api-key
                    </label>
                  </div>
                  <small id="authMode-hint">按供应商要求自动匹配。</small>
                </fieldset>
              </div>
              {target === "codex" && (
                <>
                  <p className="automatic-reasoning-hint">
                    每次写入 Codex
                    配置时自动检查并补齐思考强度选项，无需手动修复。
                  </p>
                  <fieldset
                    className="choice-field"
                    disabled={discovery.protocol === "anthropic"}
                    aria-describedby="fastMode-hint"
                  >
                    <legend>Fast 模式</legend>
                    <div className="choice-options">
                      <label>
                        <input
                          type="radio"
                          value=""
                          {...register("fastMode")}
                        />
                        跟随客户端
                      </label>
                      <label>
                        <input
                          type="radio"
                          value="on"
                          {...register("fastMode")}
                        />
                        开启
                      </label>
                      <label>
                        <input
                          type="radio"
                          value="off"
                          {...register("fastMode")}
                        />
                        关闭
                      </label>
                    </div>
                    <small id="fastMode-hint">
                      {discovery.protocol === "anthropic"
                        ? "Claude 转换暂不支持 Fast。"
                        : "需供应商支持，可能增加费用。"}
                    </small>
                  </fieldset>
                </>
              )}
              {applyOnSave && (
                <label className="save-only-option">
                  <input
                    type="checkbox"
                    checked={saveOnly}
                    onChange={(e) => setSaveOnly(e.target.checked)}
                  />
                  只保存供应商，暂不用于当前客户端
                </label>
              )}
              <AutoBalance
                input={input}
                query={preservedBalance || automaticBalanceQuery}
                revision={provider?.updatedAt}
              />
              {converted && (
                <p className="form-service-hint">
                  兼容转换会自动运行。关闭窗口后保留托盘，电脑重启后打开
                  uni-switch 即可恢复。
                </p>
              )}
            </section>
          </fieldset>
        </div>
        {!!failure && (
          <ErrorFeedback
            error={failure}
            secrets={[input.apiKey || ""]}
            onAction={
              errorCode(failure) === "external_change"
                ? undefined
                : () => {
                    const info = explainError(failure);
                    if (info.field) setFocus(info.field);
                    else formRef.current?.requestSubmit();
                  }
            }
          />
        )}
        {phase !== "idle" && (
          <p className="submit-progress" role="status">
            {phase === "models" ? "正在获取可用模型…" : "正在写入并校验配置…"}
            {slow ? " 供应商响应较慢，可以取消后稍后重试。" : ""}
          </p>
        )}
        <div className="modal-actions form-actions">
          <button
            type="button"
            className="button quiet cancel-button"
            disabled={phase === "writing"}
            onClick={close}
          >
            取消
          </button>
          <button
            type="submit"
            className="button primary"
            disabled={isSubmitting}
          >
            {phase === "models"
              ? "获取模型…"
              : phase === "writing"
                ? "写入配置…"
                : submitLabel}
            <ArrowRight size={16} aria-hidden />
          </button>
        </div>
      </form>
    </Modal>
  );
}
