import { useEffect, useRef, useState } from "react";
import { api, desktopRuntime, errorMessage } from "./api";
import { contextTokens, DEFAULT_MODEL_CONTEXT } from "./modelContext";
import { desktopModelAllowed } from "./useModelDiscovery";
import { providerProtocol } from "./protocolConversion";
import type {
  ConnectionInput,
  Provider,
  ProviderModel,
  Target,
} from "../types";
type Protocol = "openai" | "anthropic";
export type ProtocolPreference = Protocol | "auto";
export type AuthPreference = Provider["authMode"] | "auto";
export { providerProtocol } from "./protocolConversion";
export function codingModel(id: string) {
  return !/(embedding|dall-e|whisper|tts|moderation|rerank|(?:^|[-/])(image|audio|video|sora)(?:[-/]|$))/i.test(
    id,
  );
}
export function modelAllowed(id: string, target: Target, protocol: Protocol) {
  return (
    codingModel(id) &&
    (target !== "claude_desktop" ||
      protocol === "openai" ||
      desktopModelAllowed(id))
  );
}
export function recommendedModel(models: ProviderModel[], protocol: Protocol) {
  const score = (id: string) =>
    (protocol === "anthropic"
      ? /claude-sonnet/.test(id)
        ? 100
        : /claude-opus/.test(id)
          ? 90
          : /claude-haiku/.test(id)
            ? 80
            : 0
      : /(?:^|\/)gpt-/.test(id)
        ? 100
        : /(?:^|\/)o[134]-?/.test(id)
          ? 90
          : 0) +
    (/codex/i.test(id) ? 10 : 0) -
    (/mini|nano/i.test(id) ? 3 : 0);
  return (
    [...models].sort(
      (a, b) =>
        score(b.id) - score(a.id) ||
        (score(a.id)
          ? b.id.localeCompare(a.id, undefined, { numeric: true })
          : a.id.localeCompare(b.id)),
    )[0]?.id || ""
  );
}
export function useConnectionDiscovery(
  input: ConnectionInput,
  authMode: AuthPreference,
  target: Target,
  provider?: Provider,
  protocol: ProtocolPreference = "auto",
) {
  const fallbackProtocol =
    protocol !== "auto"
      ? protocol
      : provider
        ? providerProtocol(provider)
        : "openai";
  const signature = JSON.stringify([
    input.baseUrl,
    input.apiKey || `saved:${input.providerId || ""}`,
    authMode,
    target,
    protocol,
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
    /* Keep incomplete input editable. */
  }
  const initialModels = (provider?.codexOptions?.models || []).map((m) => ({
    ...m,
    contextWindow:
      target === "codex"
        ? (provider?.codexOptions?.contextWindow ??
          m.contextWindow ??
          DEFAULT_MODEL_CONTEXT)
        : m.contextWindow,
  }));
  if (provider?.model && !initialModels.some((m) => m.id === provider.model))
    initialModels.unshift({
      id: provider.model,
      enabled: true,
      contextWindow:
        target === "codex"
          ? (provider.codexOptions?.contextWindow ?? DEFAULT_MODEL_CONTEXT)
          : null,
      reasoningEfforts: [],
    });
  const [state, setState] = useState({
    signature,
    baseUrl: input.baseUrl,
    models: initialModels,
    contexts: Object.fromEntries(
      initialModels.map((m) => [
        m.id,
        String((m.contextWindow ?? DEFAULT_MODEL_CONTEXT) / 1000),
      ]),
    ),
    model: provider?.model || "",
    syncedAt: provider?.codexOptions?.modelsSyncedAt || (null as number | null),
    protocol: fallbackProtocol as Protocol,
    authMode: (authMode === "auto"
      ? provider?.authMode || "bearer"
      : authMode) as Provider["authMode"],
    busy: false,
    failure: "",
    failureError: null as unknown,
    verified: !!provider,
  });
  const stateRef = useRef(state);
  stateRef.current = state;
  const generation = useRef(0);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const current = useRef({
    input,
    authMode,
    ready,
    signature,
    protocol,
    target,
  });
  current.current = { input, authMode, ready, signature, protocol, target };
  const inFlight = useRef<{
    signature: string;
    request: number;
    promise: Promise<typeof state | null>;
  } | null>(null);
  const allows = (id: string, resolved: Protocol) =>
    modelAllowed(id, target, resolved);
  function refresh(force = true): Promise<typeof state | null> {
    if (timer.current !== null) {
      clearTimeout(timer.current);
      timer.current = null;
    }
    const params = current.current;
    if (!params.ready || !desktopRuntime) return Promise.resolve(null);
    if (
      inFlight.current?.signature === params.signature &&
      inFlight.current.request === generation.current
    )
      return inFlight.current.promise;
    if (
      !force &&
      stateRef.current.signature === params.signature &&
      stateRef.current.model &&
      stateRef.current.verified &&
      !stateRef.current.failure
    )
      return Promise.resolve(stateRef.current);
    const request = ++generation.current;
    setState((s) => ({ ...s, busy: true, failure: "" }));
    const promise = (async () => {
      try {
        const result = await api.discoverConnection(
          params.input,
          params.protocol === "auto" ? null : params.protocol,
          params.authMode === "auto" ? null : params.authMode,
        );
        if (
          request !== generation.current ||
          current.current.signature !== params.signature
        )
          return null;
        if (!result.models.length)
          throw new Error("此密钥没有返回可用模型，请检查模型访问权限");
        const resolved = result.protocol || fallbackProtocol;
        const previousState =
          stateRef.current.signature === params.signature
            ? stateRef.current
            : {
                ...stateRef.current,
                model: "",
                models: [],
                contexts: {} as Record<string, string>,
              };
        const previous = new Map(previousState.models.map((m) => [m.id, m]));
        const usable = result.models.filter((m) => allows(m.id, resolved));
        // A successful response is the current catalog. Keep choices and
        // contexts only for IDs still present; newly discovered models start
        // selected. Never append retired models from the previous snapshot.
        const selected = usable.filter(
          (m) => previous.get(m.id)?.enabled ?? true,
        );
        const model =
          selected.find((m) => m.id === previousState.model)?.id ||
          selected.find((m) => previous.get(m.id)?.enabled)?.id ||
          recommendedModel(selected, resolved);
        const next = {
          signature: params.signature,
          baseUrl: result.baseUrl || params.input.baseUrl,
          model,
          syncedAt: result.syncedAt,
          protocol: resolved,
          authMode:
            result.authMode ||
            (params.authMode === "auto"
              ? provider?.authMode || "bearer"
              : params.authMode),
          busy: false,
          verified: true,
          failureError: null,
          failure: usable.length
            ? ""
            : "未找到可用于此客户端的编码模型，请检查模型访问权限",
          contexts: Object.fromEntries(
            result.models.map((m) => [
              m.id,
              Object.hasOwn(previousState.contexts, m.id)
                ? previousState.contexts[m.id]
                : "256",
            ]),
          ),
          models: result.models.map((m) => ({
            ...m,
            contextWindow:
              target === "codex"
                ? (previous.get(m.id)?.contextWindow ?? DEFAULT_MODEL_CONTEXT)
                : (previous.get(m.id)?.contextWindow ?? m.contextWindow),
            enabled:
              allows(m.id, resolved) && (previous.get(m.id)?.enabled ?? true),
          })),
        };
        stateRef.current = next;
        setState(next);
        return next;
      } catch (error) {
        if (
          request === generation.current &&
          current.current.signature === params.signature
        ) {
          const next = {
            ...stateRef.current,
            busy: false,
            failure: errorMessage(error),
            failureError: error,
          };
          stateRef.current = next;
          setState(next);
          if (
            next.model &&
            next.models.some((m) => m.enabled) &&
            provider &&
            !params.input.apiKey &&
            params.input.baseUrl.replace(/\/+$/, "") ===
              provider.baseUrl.replace(/\/+$/, "")
          )
            return next;
        }
        return null;
      } finally {
        if (inFlight.current?.request === request) inFlight.current = null;
      }
    })();
    inFlight.current = { signature: params.signature, request, promise };
    return promise;
  }
  useEffect(() => {
    generation.current++;
    if (stateRef.current.signature !== signature) {
      const keepChoices =
        stateRef.current.baseUrl.replace(/\/+$/, "") ===
        input.baseUrl.replace(/\/+$/, "");
      const next = {
        ...stateRef.current,
        signature,
        baseUrl: input.baseUrl,
        models: keepChoices ? stateRef.current.models : [],
        contexts: keepChoices ? stateRef.current.contexts : {},
        model: keepChoices ? stateRef.current.model : "",
        syncedAt: null,
        busy: false,
        failure: "",
        failureError: null,
        verified: false,
      };
      stateRef.current = next;
      setState(next);
    }
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
  const allowed = (id: string) => allows(id, visible.protocol);
  const toggle = (id: string, enabled: boolean) =>
    setState((s) => {
      if (s.signature !== current.current.signature || !allows(id, s.protocol))
        return s;
      const models = s.models.map((m) => (m.id === id ? { ...m, enabled } : m));
      return {
        ...s,
        models,
        model: models.some((m) => m.id === s.model && m.enabled)
          ? s.model
          : models.find((m) => m.enabled)?.id || "",
      };
    });
  const selectDefault = (id: string) =>
    setState((s) =>
      s.signature === current.current.signature &&
      s.models.some((m) => m.id === id) &&
      allows(id, s.protocol)
        ? {
            ...s,
            model: id,
            models: s.models.map((m) =>
              m.id === id ? { ...m, enabled: true } : m,
            ),
          }
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
  const invalidContexts =
    target === "codex"
      ? visible.models
          .filter(
            (m) =>
              m.enabled &&
              contextTokens(visible.contexts[m.id] ?? "256") === null,
          )
          .map((m) => m.id)
      : [];
  return {
    ...visible,
    ready,
    refresh,
    ensureReady: () => refresh(false),
    getFailure: () => stateRef.current.failureError,
    toggle,
    selectDefault,
    setContext,
    invalidContexts,
    allowed,
    preview: !desktopRuntime,
  };
}
