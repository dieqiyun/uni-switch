import { useEffect, useId, useRef, useState } from "react";
import { api, errorMessage } from "../lib/api";
import { effectiveProfile } from "../lib/modelCapabilities";
import type { useConnectionDiscovery } from "../lib/useConnectionDiscovery";
import type { ModelProfile, ModelVerification, ProviderModel } from "../types";

export function ModelProfileDetails({
  model,
  discovery,
}: {
  model: ProviderModel;
  discovery: ReturnType<typeof useConnectionDiscovery>;
}) {
  const profile = effectiveProfile(model);
  const id = useId();
  const [open, setOpen] = useState(false);
  const [endpoint, setEndpoint] = useState<ModelVerification["endpoint"]>(
    discovery.protocol === "anthropic"
      ? "messages"
      : profile.endpoints?.responses === false &&
          profile.endpoints.chatCompletions === true
        ? "chat_completions"
        : "responses",
  );
  const [feature, setFeature] = useState<ModelVerification["feature"]>("text");
  const [consent, setConsent] = useState(false);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState("");
  const signature = JSON.stringify([
    discovery.connection,
    discovery.authMode,
    endpoint,
    feature,
    model.id,
  ]);
  const current = useRef(signature);
  const generation = useRef(0);
  current.current = signature;
  useEffect(() => {
    current.current = signature;
    generation.current++;
    setResult("");
    setConsent(false);
    setBusy(false);
    return () => {
      current.current = "";
      generation.current++;
    };
  }, [signature]);
  const label = (supported: boolean | null | undefined) =>
    supported == null ? "待确认" : supported ? "已声明支持" : "明确不支持";
  async function verify() {
    if (!consent || busy) return;
    const expected = signature;
    const request = ++generation.current;
    const active = () =>
      current.current === expected && generation.current === request;
    setBusy(true);
    setResult("");
    try {
      const observation = await api.verifyModel(
        discovery.connection,
        model.id,
        discovery.authMode,
        endpoint,
        feature,
        consent,
      );
      if (active()) setResult(observation.message);
    } catch (error) {
      if (active()) setResult(errorMessage(error));
    } finally {
      if (active()) setBusy(false);
    }
  }
  return (
    <details
      className="model-profile-details"
      onToggle={(event) => setOpen(event.currentTarget.open)}
    >
      <summary>接口、思考参数与接入验证</summary>
      {open && (
        <div
          className="model-profile-content"
          aria-label={`${model.id} 能力详情`}
          aria-busy={busy}
        >
          <p>
            Messages：{label(profile.endpoints?.messages)} · Chat Completions：
            {label(profile.endpoints?.chatCompletions)} · Responses：
            {label(profile.endpoints?.responses)}
          </p>
          <p>
            工具调用：{label(profile.toolCalls)} · 结构化输出：
            {label(profile.structuredOutput)}
          </p>
          <p>
            资料中的上下文：
            {profile.contextWindow?.toLocaleString() ?? "待确认"} · 最大输出：
            {profile.maxOutputTokens?.toLocaleString() ?? "待确认"}{" "}
            Token。仅展示资料，不修改当前上下文设置。
          </p>
          <p>
            上游/官方思考档位：
            {profile.reasoningEfforts?.join(" / ") ||
              (profile.reasoningEfforts ? "不支持" : "待确认")}
            ；默认：{profile.defaultEffort ?? "待确认"}
            。客户端八档菜单保持不变，转换请求按档案映射。
          </p>
          <label htmlFor={`${id}-thinking`}>
            转换请求的思考模式（{model.id}）
          </label>
          <select
            id={`${id}-thinking`}
            value={model.profileOverrides?.thinkingFormat ?? "auto"}
            disabled={discovery.busy}
            onChange={(event) =>
              discovery.setThinkingFormat(
                model.id,
                event.target.value === "auto"
                  ? null
                  : (event.target.value as ModelProfile["thinkingFormat"]),
              )
            }
          >
            <option value="auto">
              自动（{profile.thinkingFormat ?? "待确认"}）
            </option>
            <option value="adaptive">Claude 自适应思考</option>
            <option value="budget">Claude 预算思考</option>
            <option value="deepseek">DeepSeek 思考参数</option>
            <option value="openai">OpenAI 思考参数</option>
            <option value="none">不支持思考</option>
          </select>
          <p>
            {model.profileOverrides?.thinkingFormat
              ? "思考模式：手动设置"
              : model.profile?.thinkingFormat
                ? "思考模式：上游返回"
                : "思考模式：官方资料或待确认"}
          </p>
          {model.officialSource && (
            <p className="model-profile-source">
              官方资料来源：<span>{model.officialSource}</span>
            </p>
          )}
          {model.metadataUpdatedAt && (
            <p>
              上游资料更新时间：
              {new Date(model.metadataUpdatedAt * 1000).toLocaleString()}
            </p>
          )}
          <fieldset
            disabled={busy || discovery.busy || discovery.preview}
            aria-describedby={`${id}-warning`}
          >
            <legend>验证 {model.id} 的接入能力</legend>
            <label htmlFor={`${id}-endpoint`}>验证接口</label>
            <select
              id={`${id}-endpoint`}
              value={endpoint}
              onChange={(event) =>
                setEndpoint(event.target.value as ModelVerification["endpoint"])
              }
            >
              <option value="messages">Claude Messages</option>
              <option value="chat_completions">Chat Completions</option>
              <option value="responses">Responses</option>
            </select>
            <label htmlFor={`${id}-feature`}>验证项目</label>
            <select
              id={`${id}-feature`}
              value={feature}
              onChange={(event) =>
                setFeature(event.target.value as ModelVerification["feature"])
              }
            >
              <option value="text">文本回复</option>
              <option value="image">图片理解</option>
              <option value="tools">合成工具调用（不执行工具）</option>
              <option value="stream">流式回复与结束事件</option>
            </select>
            <p id={`${id}-warning`}>
              仅发送一条合成请求，最多请求 256 个输出
              Token，可能计费。不会发送会话、文件或执行工具；结果只代表本次条件，不自动覆盖设置。
            </p>
            <label>
              <input
                type="checkbox"
                checked={consent}
                onChange={(event) => setConsent(event.target.checked)}
              />
              我确认发送验证请求并接受可能的费用
            </label>
            <button
              type="button"
              className="button secondary"
              disabled={!consent}
              onClick={() => void verify()}
            >
              {busy ? "验证中…" : "开始验证"}
            </button>
          </fieldset>
          {discovery.preview && <p>浏览器预览不发送验证请求。</p>}
          <p role="status" aria-live="polite">
            {result}
          </p>
        </div>
      )}
    </details>
  );
}
