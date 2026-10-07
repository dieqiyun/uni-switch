import { useEffect, useRef, useState } from "react";
import { api, desktopRuntime, errorMessage } from "./api";
import { contextTokens, DEFAULT_MODEL_CONTEXT } from "./modelContext";
import { preserveCapabilities, type CapabilityKey } from "./modelCapabilities";
import type {
  ConnectionInput,
  Provider,
  ProviderModel,
  Target,
} from "../types";

export function desktopModelAllowed(id: string) {
  return /^(anthropic\/)?claude-(sonnet|opus|haiku|fable)-.+$/.test(id);
}
export function useModelDiscovery(
  input: ConnectionInput,
  authMode: Provider["authMode"],
  target: Target,
  provider?: Provider,
  protocol?: "openai" | "anthropic",
) {
  const signature = JSON.stringify([
    input.baseUrl,
    input.apiKey || `saved:${input.providerId || ""}`,
    authMode,
    target,
    protocol || "openai",
  ]);
  let ready = false;
  try {
    const url = new URL(input.baseUrl);
    ready =
      ["http:", "https:"].includes(url.protocol) &&
      !!url.hostname &&
      !url.username &&
      !url.password &&
      !url.search &&
      !url.hash &&
      !!(input.apiKey?.trim() || input.providerId) &&
      !/[\u0000-\u001f\u007f]/.test(input.apiKey || "");
  } catch {
    /* Incomplete input remains editable. */
  }
  const allowed = (id: string) =>
    target !== "claude_desktop" ||
    protocol === "openai" ||
    desktopModelAllowed(id);
  const [state, setState] = useState(() => {
    const models = (provider?.codexOptions?.models || []).map((m) => ({
      ...m,
      contextWindow:
        target === "codex"
          ? (provider?.codexOptions?.contextWindow ??
            m.contextWindow ??
            DEFAULT_MODEL_CONTEXT)
          : m.contextWindow,
    }));
    if (provider?.model && !models.some((m) => m.id === provider.model))
      models.unshift({
        id: provider.model,
        enabled: true,
        contextWindow:
          target === "codex"
            ? (provider?.codexOptions?.contextWindow ?? DEFAULT_MODEL_CONTEXT)
            : null,
        reasoningEfforts: [],
      });
    return {
      signature,
      models,
      contexts: Object.fromEntries(
        models.map((m) => [
          m.id,
          String((m.contextWindow ?? DEFAULT_MODEL_CONTEXT) / 1000),
        ]),
      ),
      model: provider?.model || "",
      syncedAt:
        provider?.codexOptions?.modelsSyncedAt || (null as number | null),
      busy: false,
      failure: "",
    };
  });
  const generation = useRef(0);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const current = useRef({ input, authMode, ready, signature, protocol });
  current.current = { input, authMode, ready, signature, protocol };
  async function refresh() {
    if (timer.current !== null) {
      clearTimeout(timer.current);
      timer.current = null;
    }
    const params = current.current;
    if (!params.ready || !desktopRuntime) return;
    const requestGeneration = ++generation.current;
    setState((s) => ({ ...s, busy: true, failure: "" }));
    try {
      const result =
        params.protocol === "anthropic"
          ? await api.syncModels(params.input, params.authMode, params.protocol)
          : await api.syncModels(params.input, params.authMode);
      if (
        requestGeneration !== generation.current ||
        current.current.signature !== params.signature
      )
        return;
      if (!result.models.length)
        throw new Error("此密钥没有返回可用模型，请检查模型访问权限");
      setState((s) => {
        const previous = new Map(s.models.map((m) => [m.id, m]));
        const usable = result.models.filter((m) => allowed(m.id));
        const selected = usable.filter(
          (m) => previous.get(m.id)?.enabled ?? true,
        );
        const model =
          selected.find((m) => m.id === s.model)?.id ||
          selected.find((m) => previous.get(m.id)?.enabled)?.id ||
          selected[0]?.id ||
          "";
        return {
          signature: params.signature,
          model,
          syncedAt: result.syncedAt,
          busy: false,
          failure: usable.length
            ? ""
            : "未找到可用于 Claude 桌面端的模型，请检查供应商的 Claude 接入地址",
          contexts: Object.fromEntries(
            result.models.map((m) => [
              m.id,
              Object.hasOwn(s.contexts, m.id) ? s.contexts[m.id] : "256",
            ]),
          ),
          models: result.models.map((m) => ({
            ...preserveCapabilities(m, previous.get(m.id)),
            contextWindow:
              target === "codex"
                ? previous.has(m.id)
                  ? previous.get(m.id)!.contextWindow
                  : DEFAULT_MODEL_CONTEXT
                : m.contextWindow,
            enabled: allowed(m.id) && (previous.get(m.id)?.enabled ?? true),
          })),
        };
      });
    } catch (e) {
      if (
        requestGeneration === generation.current &&
        current.current.signature === params.signature
      )
        setState((s) => ({ ...s, busy: false, failure: errorMessage(e) }));
    }
  }
  useEffect(() => {
    generation.current++;
    setState((s) =>
      s.signature === signature
        ? s
        : {
            signature,
            models: [],
            contexts: {},
            model: "",
            syncedAt: null,
            busy: false,
            failure: "",
          },
    );
    if (ready && desktopRuntime)
      timer.current = setTimeout(() => void refresh(), 650);
    return () => {
      generation.current++;
      if (timer.current !== null) clearTimeout(timer.current);
      timer.current = null;
    };
  }, [signature, ready]);
  const visible =
    state.signature === signature
      ? state
      : {
          ...state,
          models: [],
          contexts: {},
          model: "",
          syncedAt: null,
          busy: false,
          failure: "",
        };
  const toggle = (id: string, enabled: boolean) =>
    setState((s) => {
      if (s.signature !== current.current.signature || !allowed(id)) return s;
      const models = s.models.map((m) => (m.id === id ? { ...m, enabled } : m));
      const model = models.some((m) => m.id === s.model && m.enabled)
        ? s.model
        : models.find((m) => m.enabled)?.id || "";
      return { ...s, models, model };
    });
  const selectDefault = (id: string) =>
    setState((s) =>
      s.signature === current.current.signature &&
      s.models.some((m) => m.id === id && m.enabled)
        ? { ...s, model: id }
        : s,
    );
  const setContext = (id: string, value: string) =>
    setState((s) =>
      s.signature === current.current.signature && target === "codex"
        ? {
            ...s,
            contexts: { ...s.contexts, [id]: value },
            models: s.models.map((m) =>
              m.id === id ? { ...m, contextWindow: contextTokens(value) } : m,
            ),
          }
        : s,
    );
  const setCapability = (id: string, key: CapabilityKey, value: boolean) =>
    setState((s) =>
      s.signature === current.current.signature
        ? {
            ...s,
            models: s.models.map((m) =>
              m.id === id
                ? {
                    ...m,
                    capabilityOverrides: {
                      ...m.capabilityOverrides,
                      [key]: value,
                    },
                  }
                : m,
            ),
          }
        : s,
    );
  const resetCapabilities = (id: string) =>
    setState((s) =>
      s.signature === current.current.signature
        ? {
            ...s,
            models: s.models.map((m) =>
              m.id === id ? { ...m, capabilityOverrides: {} } : m,
            ),
          }
        : s,
    );
  const invalidContexts =
    target === "codex"
      ? visible.models
          .filter(
            (m) => contextTokens(visible.contexts[m.id] ?? "256") === null,
          )
          .map((m) => m.id)
      : [];
  return {
    ...visible,
    ready,
    refresh,
    toggle,
    selectDefault,
    setContext,
    setCapability,
    resetCapabilities,
    invalidContexts,
    allowed,
    preview: !desktopRuntime,
  };
}
