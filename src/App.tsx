import { useEffect, useRef, useState } from "react";
import "./client-config.css";
import * as Tabs from "@radix-ui/react-tabs";
import { useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowRight,
  Check,
  CircleHelp,
  Monitor,
  Pencil,
  Plus,
  RefreshCw,
  RotateCcw,
  Search,
  ShieldCheck,
  SlidersHorizontal,
  Terminal,
  Trash2,
  Unplug,
  Zap,
  Pin,
  X,
} from "lucide-react";
import { api, desktopRuntime, errorMessage } from "./lib/api";
import { APP_VERSION } from "./lib/appVersion";
import { useAppUpdate } from "./lib/useAppUpdate";
import { AppUpdateEntry } from "./components/AppUpdateEntry";
import { AppUpdatePanel } from "./components/AppUpdatePanel";
import { ProviderProtocolControl } from "./components/ProviderProtocolControl";
import { ServicePromotion } from "./components/ServicePromotion";
import appLogo from "./assets/uni-switch-logo.png";
import {
  needsProtocolConversion,
  protocolConfirmed,
  protocolConversionEnabled,
} from "./lib/protocolConversion";
import {
  loadTarget,
  rememberTarget,
  loadListPreferences,
  rememberListPreferences,
} from "./lib/uiPreferences";
import { providerProtocol } from "./lib/useConnectionDiscovery";
import {
  providerState,
  providerMatches,
  displayProviderName,
  explainError,
  errorCode,
} from "./lib/feedback";
import { ErrorFeedback } from "./components/ErrorFeedback";
import { ProviderForm } from "./components/ProviderForm";
import { ProviderBalance } from "./components/ProviderBalance";
import {
  ClientRestartDialog,
  needsRestart,
  restartNames,
} from "./components/ClientRestartDialog";
import { Modal } from "./components/Modal";
import { TutorialDialog } from "./components/TutorialDialog";
import { WindowChrome } from "./components/WindowChrome";
import { ProviderName } from "./components/ProviderName";
import { ModelRegistryPanel } from "./components/ModelRegistryPanel";
import {
  ClientConfigEditor,
  ConfigRestartNotice,
} from "./components/ClientConfigEditor";
import { ExtraClientPanel } from "./components/ExtraClientPanel";
import { installRegistry } from "./lib/modelCapabilities";
import {
  ProviderModelSelect,
  ProviderModelsDialog,
} from "./components/ProviderQuickSettings";
import codexLogo from "./assets/brands/codex.png";
import claudeLogo from "./assets/brands/claude.png";
import zcodeLogo from "./assets/brands/zcode.png";
import dshLogo from "./assets/brands/dsh.svg";
import workbuddyLogo from "./assets/brands/workbuddy.svg";
import {
  targetNames,
  clientNames,
  type ClientKind,
  type ExtraClient,
  type ConfigWriteResult,
  type Family,
  type Provider,
  type RuntimeStatus,
  type Target,
  type QuickModelInput,
  type ApplyOverwriteConfirmation,
} from "./types";

type Popup =
  | { kind: "config-editor"; client: ClientKind }
  | { kind: "config-restart"; client: ClientKind; result: ConfigWriteResult }
  | { kind: "create" }
  | { kind: "edit"; provider: Provider; models?: boolean }
  | { kind: "models"; provider: Provider; target: Target }
  | { kind: "delete"; provider: Provider }
  | { kind: "restore" }
  | { kind: "settings" }
  | { kind: "update" }
  | { kind: "directory" }
  | { kind: "conflict" }
  | {
      kind: "overwrite";
      provider: Provider;
      confirmation: ApplyOverwriteConfirmation;
    }
  | { kind: "help"; topic?: string }
  | { kind: "restart-client"; runtime: RuntimeStatus }
  | null;
function Glyph({ family }: { family: Family }) {
  return (
    <span className="app-glyph">
      <img
        src={family === "codex" ? codexLogo : claudeLogo}
        width="28"
        height="28"
        alt=""
        aria-hidden
      />
    </span>
  );
}
export default function App() {
  useEffect(() => {
    let active = true;
    void api
      .modelRegistry()
      .then((status) => {
        if (active) installRegistry(status.registry);
      })
      .catch(() => {});
    return () => {
      active = false;
    };
  }, []);
  const [family, setFamily] = useState<Family>(() => loadTarget().family);
  const [extraClient, setExtraClient] = useState<ExtraClient | null>(null);
  const [claudeTarget, setClaudeTarget] = useState<Target>(
    () => loadTarget().claudeTarget,
  );
  useEffect(() => rememberTarget(family, claudeTarget), [family, claudeTarget]);
  const [search, setSearch] = useState("");
  const [searchExpanded, setSearchExpanded] = useState(false);
  const searchInput = useRef<HTMLInputElement>(null);
  const [renameProviderId, setRenameProviderId] = useState<string | null>(null);
  const [popup, setPopup] = useState<Popup>(null);
  const [notice, setNotice] = useState<{
    error: boolean;
    text: string;
    details?: unknown;
    syncProviderId?: string;
    onAction?: () => void;
  } | null>(null);
  const [busy, setBusy] = useState(false);
  const [updateBusy, setUpdateBusy] = useState(false);
  const [busyProviderId, setBusyProviderId] = useState<string | null>(null);
  const [fastProviderId, setFastProviderId] = useState<string | null>(null);
  const [conversionProviderId, setConversionProviderId] = useState<
    string | null
  >(null);
  const [quickProviderId, setQuickProviderId] = useState<string | null>(null);
  const [listPreferences, setListPreferences] = useState(loadListPreferences);
  useEffect(() => rememberListPreferences(listPreferences), [listPreferences]);
  useEffect(() => {
    if (!notice || notice.error || notice.syncProviderId) return;
    const timer = setTimeout(() => setNotice(null), 4500);
    return () => clearTimeout(timer);
  }, [notice]);
  const [directory, setDirectory] = useState("");
  const commitResult = useRef<{
    provider: Provider;
    pending: boolean;
    failures: string[];
  } | null>(null);
  const [compact, setCompact] = useState(
    () => window.matchMedia?.("(max-width: 800px)").matches ?? false,
  );
  useEffect(() => {
    const media = window.matchMedia("(max-width: 800px)");
    const update = () => setCompact(media.matches);
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, []);
  const client = useQueryClient();
  const appUpdate = useAppUpdate();
  const query = useQuery({
    queryKey: ["overview"],
    queryFn: api.overview,
    refetchInterval: desktopRuntime ? 15000 : false,
  });
  const target: Target = family === "codex" ? "codex" : claudeTarget;
  useEffect(() => {
    if (searchExpanded) searchInput.current?.focus();
  }, [searchExpanded]);
  useEffect(() => {
    const focusSearch = (event: KeyboardEvent) => {
      if (
        !(event.ctrlKey || event.metaKey) ||
        event.key.toLowerCase() !== "f" ||
        popup ||
        busy ||
        !query.data?.providers.length
      )
        return;
      event.preventDefault();
      setSearchExpanded(true);
      searchInput.current?.focus();
    };
    window.addEventListener("keydown", focusSearch);
    return () => window.removeEventListener("keydown", focusSearch);
  }, [popup, busy, query.data?.providers.length]);
  const runtimeTargets: Target[] = ["codex", "claude_desktop", "claude_cli"];
  const runtimes = useQueries({
    queries: runtimeTargets.map((runtimeTarget) => ({
      queryKey: ["runtime", runtimeTarget],
      queryFn: () => api.runtime(runtimeTarget),
      enabled: desktopRuntime,
      refetchInterval: 10000,
    })),
  });
  const runtime = runtimes[runtimeTargets.indexOf(target)];
  const status = query.data?.targets.find((t) => t.target === target);
  const promptedRevision = useRef<Partial<Record<Target, number>>>({});
  const codexRuntime = runtimes[0].data;
  const claudeDesktopRuntime = runtimes[1].data;
  const claudeCliRuntime = runtimes[2].data;
  const observedCodex = useRef<{ directory: string; revision: number } | null>(
    null,
  );
  const observedClaudeWrites = useRef<
    Partial<Record<Target, { directory: string; revision: number }>>
  >({});
  const [claudeWriteRevisions, setClaudeWriteRevisions] = useState<
    Partial<Record<Target, number>>
  >({});
  useEffect(() => {
    if (!desktopRuntime) return;
    for (const value of query.data?.targets ?? []) {
      if (value.target === "codex" || value.configurationRevision === undefined)
        continue;
      const previous = observedClaudeWrites.current[value.target];
      const revision = value.configurationRevision;
      observedClaudeWrites.current[value.target] = {
        directory: value.directory,
        revision,
      };
      if (
        !previous ||
        previous.directory !== value.directory ||
        revision < previous.revision
      ) {
        setClaudeWriteRevisions((pending) => {
          const next = { ...pending };
          delete next[value.target];
          return next;
        });
        if (previous?.directory !== value.directory)
          delete promptedRevision.current[value.target];
      } else if (revision > previous.revision) {
        setClaudeWriteRevisions((pending) => ({
          ...pending,
          [value.target]: revision,
        }));
      }
    }
  }, [query.data]);
  const [codexWriteRevision, setCodexWriteRevision] = useState<number | null>(
    null,
  );
  useEffect(() => {
    if (!desktopRuntime) return;
    const codexStatus = query.data?.targets.find(
      (value) => value.target === "codex",
    );
    if (!codexStatus || codexStatus.configurationRevision === undefined) return;
    const previous = observedCodex.current;
    const revision = codexStatus.configurationRevision;
    observedCodex.current = { directory: codexStatus.directory, revision };
    if (
      !previous ||
      previous.directory !== codexStatus.directory ||
      revision < previous.revision
    ) {
      setCodexWriteRevision(null);
      if (previous && previous.directory !== codexStatus.directory) {
        delete promptedRevision.current.codex;
      }
    } else if (revision > previous.revision) {
      setCodexWriteRevision(revision);
    }
  }, [query.data]);
  useEffect(() => {
    if (query.data?.repairedModelCapabilities) {
      const repaired = query.data.targets.find(
        (value) => value.target === "codex",
      )?.configurationRevision;
      if (repaired && repaired > (promptedRevision.current.codex ?? -1))
        setCodexWriteRevision(repaired);
    }
  }, [query.data?.repairedModelCapabilities]);
  useEffect(() => {
    if (popup || busy) return;
    // A successful Codex file write always prompts, including when process
    // inspection fails or no desktop process is visible. The overview carries
    // the actual write revision independently of runtime inspection.
    if (codexWriteRevision !== null) {
      setCodexWriteRevision(null);
      if (codexWriteRevision <= (promptedRevision.current.codex ?? -1)) return;
      const value: RuntimeStatus =
        codexRuntime?.configurationRevision === codexWriteRevision
          ? { ...codexRuntime }
          : {
              target: "codex",
              configurationRevision: codexWriteRevision,
              clientRunning: false,
              restartRequired: false,
              bridgeRequired: false,
              bridgeHealthy: true,
              desktopRunning: false,
              desktopRestartRequired: false,
              canRestartDesktop: false,
              canRestartClient: false,
              restartReason:
                "尚未确认可自动重启的 Codex 桌面实例。请手动重启已打开的 Codex；CLI 请退出旧进程后重新运行。",
            };
      promptedRevision.current.codex = codexWriteRevision;
      setPopup({ kind: "restart-client", runtime: value });
      return;
    }
    for (const clientTarget of ["claude_desktop", "claude_cli"] as const) {
      const revision = claudeWriteRevisions[clientTarget];
      if (revision === undefined) continue;
      setClaudeWriteRevisions((pending) => {
        const next = { ...pending };
        delete next[clientTarget];
        return next;
      });
      if (revision <= (promptedRevision.current[clientTarget] ?? -1)) continue;
      const current =
        clientTarget === "claude_desktop"
          ? claudeDesktopRuntime
          : claudeCliRuntime;
      const value: RuntimeStatus =
        current?.configurationRevision === revision
          ? { ...current }
          : {
              target: clientTarget,
              configurationRevision: revision,
              clientRunning: false,
              restartRequired: false,
              bridgeRequired: false,
              bridgeHealthy: true,
              desktopRunning: false,
              desktopRestartRequired: false,
              canRestartClient: false,
            };
      promptedRevision.current[clientTarget] = revision;
      setPopup({ kind: "restart-client", runtime: value });
      return;
    }
    const pending = [codexRuntime, claudeDesktopRuntime, claudeCliRuntime]
      .filter((value): value is RuntimeStatus => !!value)
      .sort(
        (a, b) => Number(b.target === target) - Number(a.target === target),
      );
    for (const value of pending) {
      const revision = value.configurationRevision ?? 0;
      if (
        needsRestart(value) &&
        revision > (promptedRevision.current[value.target] ?? -1)
      ) {
        promptedRevision.current[value.target] = revision;
        setPopup({ kind: "restart-client", runtime: value });
        break;
      }
    }
  }, [
    codexRuntime,
    claudeDesktopRuntime,
    claudeCliRuntime,
    target,
    popup,
    busy,
    codexWriteRevision,
    claudeWriteRevisions,
  ]);
  const background = useQuery({
    queryKey: ["background"],
    queryFn: api.background,
    enabled: desktopRuntime,
  });
  const product = family === "codex" ? "Codex" : "Claude Code";
  const providers = query.data?.providers || [];
  const initialOrder = useRef<{ target: Target; ids: string[] }>({
    target,
    ids: [],
  });
  if (
    initialOrder.current.target !== target ||
    (!initialOrder.current.ids.length && providers.length)
  ) {
    initialOrder.current = {
      target,
      ids: [...providers]
        .sort(
          (a, b) =>
            Number(b.id === status?.activeProviderId) -
              Number(a.id === status?.activeProviderId) ||
            (listPreferences.recent[b.id] || 0) -
              (listPreferences.recent[a.id] || 0),
        )
        .map((p) => p.id),
    };
  }
  const visible = providers
    .filter((p) => providerMatches(p, search))
    .sort(
      (a, b) =>
        Number(listPreferences.pinned.includes(b.id)) -
          Number(listPreferences.pinned.includes(a.id)) ||
        initialOrder.current.ids.indexOf(a.id) -
          initialOrder.current.ids.indexOf(b.id),
    );
  const applied = status?.state === "applied";
  const activeState = providerState(status, runtime.data, target);
  async function syncProvider(providerId: string) {
    setBusy(true);
    setBusyProviderId(providerId);
    try {
      const results = await api.syncTargets(providerId);
      const failures = results.filter((r) => !r.success);
      const successes = results
        .filter((r) => r.success)
        .map((r) => targetNames[r.target]);
      setNotice({
        error: !!failures.length,
        text:
          [
            successes.length ? "已更新：" + successes.join("、") : "",
            ...failures.map(
              (r) =>
                targetNames[r.target] + "：" + explainError(r.error).message,
            ),
          ]
            .filter(Boolean)
            .join("；") || "所有已使用客户端均为最新配置。",
        syncProviderId: failures.length ? providerId : undefined,
      });
      await client.invalidateQueries({ queryKey: ["overview"] });
      await client.invalidateQueries({ queryKey: ["runtime"] });
    } catch (error) {
      setNotice({
        error: true,
        text: explainError(error).message,
        details: error,
        syncProviderId: providerId,
      });
    } finally {
      setBusy(false);
      setBusyProviderId(null);
    }
  }
  async function action<T>(
    task: () => Promise<T>,
    success: string | ((result: T) => string),
    close = false,
    providerId: string | null = null,
    onConflict?: (error: unknown) => Promise<void>,
  ) {
    setBusy(true);
    setBusyProviderId(providerId);
    setNotice(null);
    try {
      const result = await task();
      if (providerId)
        setListPreferences((p) => ({
          ...p,
          recent: { ...p.recent, [providerId]: Date.now() },
        }));
      await client.invalidateQueries({ queryKey: ["overview"] });
      await client.invalidateQueries({ queryKey: ["runtime"] });
      setNotice({
        error: false,
        text: typeof success === "function" ? success(result) : success,
      });
      if (close) setPopup(null);
      return result;
    } catch (error) {
      if (
        onConflict &&
        ["external_change", "overwrite_confirmation_changed"].includes(
          errorCode(error),
        )
      ) {
        try {
          await onConflict(error);
          return;
        } catch (preparationError) {
          error = preparationError;
        }
      }
      setNotice({
        error: true,
        text: explainError(error).message,
        details: error,
        onAction: () => {
          const code = errorCode(error);
          if (code === "external_change") setPopup({ kind: "conflict" });
          else if (
            code === "provider_changed" ||
            code === "configuration_changed"
          ) {
            setPopup(null);
            void client.invalidateQueries({ queryKey: ["overview"] });
          } else if (
            code === "directory_selection" ||
            code === "discovery_changed"
          )
            setPopup({ kind: "settings" });
          else if (explainError(error).field && providerId) {
            const provider = providers.find((p) => p.id === providerId);
            if (provider) setPopup({ kind: "edit", provider });
          } else void action(task, success, close, providerId, onConflict);
        },
      });
    } finally {
      setBusy(false);
      setBusyProviderId(null);
    }
  }
  async function useProvider(
    provider: Provider,
    applyTarget: Target,
    confirmation?: ApplyOverwriteConfirmation,
  ) {
    await action(
      () =>
        confirmation
          ? api.apply(applyTarget, provider.id, confirmation.token)
          : api.apply(applyTarget, provider.id),
      desktopRuntime
        ? `配置已写入 ${targetNames[applyTarget]}。请按提示重启并新开对话；旧会话可能继续沿用原模型和思考强度。`
        : "预览：已选择配置，未修改客户端文件。",
      !!confirmation,
      provider.id,
      async (error) => {
        const prepared = await api.prepareOverwrite(applyTarget, provider.id);
        if (
          prepared.target !== applyTarget ||
          prepared.providerId !== provider.id
        )
          throw new Error("配置确认信息已变化，请重新点击使用。");
        const latest = await api.overview();
        const currentProvider = latest.providers.find(
          (value) => value.id === provider.id,
        );
        if (!currentProvider) throw new Error("供应商已被删除，请刷新列表。");
        client.setQueryData(["overview"], latest);
        setBusy(false);
        setBusyProviderId(null);
        setPopup({
          kind: "overwrite",
          provider: currentProvider,
          confirmation: prepared,
        });
        setNotice(
          confirmation
            ? {
                error: true,
                text: "确认期间配置又发生变化，或确认已过期。此次没有写入，请检查后再次确认。",
                details: error,
              }
            : null,
        );
      },
    );
  }
  async function quickModels(input: QuickModelInput, label: string) {
    setQuickProviderId(input.expected.id);
    try {
      const result = await action(
        () => api.quickModels(input),
        (saved) =>
          desktopRuntime
            ? `${label}。${saved.applied ? `已写入 ${targetNames[input.target]}，请按提示加载。` : "下次使用此供应商时生效。"}`
            : `预览：${label}，未修改客户端文件。`,
        false,
        input.expected.id,
      );
      return !!result;
    } finally {
      setQuickProviderId(null);
    }
  }
  async function toggleFast(provider: Provider) {
    const enabled = provider.codexOptions?.fastMode !== true;
    setFastProviderId(provider.id);
    try {
      await action(
        () => api.setFastMode(provider.id, enabled),
        (result) =>
          desktopRuntime
            ? `Fast 已${enabled ? "开启" : "关闭"}。${result.applied ? "已写入 Codex，请重新打开客户端加载配置。" : "下次在 Codex 使用此供应商时生效。"}`
            : `预览：Fast 已${enabled ? "开启" : "关闭"}，未修改客户端文件。`,
        false,
        provider.id,
      );
    } finally {
      setFastProviderId(null);
    }
  }
  async function toggleConversion(provider: Provider, enabled: boolean) {
    setConversionProviderId(provider.id);
    try {
      await action(
        () => api.setProtocolConversion(provider, target, enabled),
        (result) =>
          result.restored
            ? `协议转换已关闭，已停用此供应商并恢复 ${targetNames[target]} 原来的 API 配置。请按提示重启客户端。`
            : `协议转换已${enabled ? "开启" : "关闭"}。${enabled ? "点击使用即可自动转换。" : "此客户端暂不使用该供应商，其他客户端不受影响。"}`,
        false,
        provider.id,
      );
    } finally {
      setConversionProviderId(null);
    }
  }
  function open(value: Popup, event?: React.MouseEvent<HTMLButtonElement>) {
    document
      .querySelector("[data-dialog-return]")
      ?.removeAttribute("data-dialog-return");
    event?.currentTarget.setAttribute("data-dialog-return", "");
    setNotice(null);
    setPopup(value);
  }
  function openUpdates(event: React.MouseEvent<HTMLButtonElement>) {
    open({ kind: "update" }, event);
    if (!desktopRuntime || appUpdate.source.isFetching) return;
    if (appUpdate.source.error) {
      void appUpdate.source.refetch();
    } else if (
      appUpdate.source.data?.repository &&
      !appUpdate.check.isFetching
    ) {
      void appUpdate.check.refetch();
    }
  }
  const switchFamily = (value: string) => {
    if (["zcode", "dsh", "workbuddy"].includes(value)) {
      setExtraClient(value as ExtraClient);
      setNotice(null);
      return;
    }
    setExtraClient(null);
    setFamily(value as Family);
    setSearch("");
    setSearchExpanded(false);
    setNotice(null);
  };
  const changeTarget = (value: Target) => {
    setClaudeTarget(value);
    setNotice(null);
  };
  return (
    <Tabs.Root
      value={extraClient ?? family}
      onValueChange={switchFamily}
      orientation={compact ? "horizontal" : "vertical"}
      className={`app-shell${desktopRuntime ? " desktop-shell" : ""}`}
    >
      <WindowChrome />
      <aside className="sidebar" aria-label="客户端导航">
        <div className="brand" data-tauri-drag-region="deep">
          <img
            className="brand-logo"
            src={appLogo}
            alt=""
            width="32"
            height="32"
            draggable={false}
          />
          <span>
            uni<span className="brand-light">switch</span>
          </span>
        </div>
        <p className="nav-caption">选择客户端</p>
        <Tabs.List className="app-nav" aria-label="选择编程工具">
          {(["codex", "claude"] as Family[]).map((item) => (
            <Tabs.Trigger key={item} value={item} disabled={busy}>
              <Glyph family={item} />
              <span className="nav-copy">
                <strong>{item === "codex" ? "Codex" : "Claude Code"}</strong>
                <small>桌面端与 CLI</small>
              </span>
              <span className="nav-indicator" aria-hidden />
            </Tabs.Trigger>
          ))}
          {(["zcode", "dsh", "workbuddy"] as const).map((item) => (
            <Tabs.Trigger key={item} value={item} disabled={busy}>
              <span className="app-glyph" aria-hidden>
                <img
                  src={
                    item === "zcode"
                      ? zcodeLogo
                      : item === "dsh"
                        ? dshLogo
                        : workbuddyLogo
                  }
                  alt=""
                  width="28"
                  height="28"
                  draggable={false}
                />
              </span>
              <span className="nav-copy">
                <strong>{clientNames[item]}</strong>
                <small>
                  {item === "dsh" ? "DeepSeek Harness" : "一键配置"}
                </small>
              </span>
              <span className="nav-indicator" aria-hidden />
            </Tabs.Trigger>
          ))}
        </Tabs.List>
        <div className="sidebar-bottom">
          <button
            className="help-link"
            onClick={(e) => open({ kind: "help" }, e)}
          >
            <CircleHelp size={18} aria-hidden />
            使用说明
            <ArrowRight size={15} aria-hidden />
          </button>
          <div className="local-note">
            <ShieldCheck size={15} aria-hidden />
            <span>密钥仅保存在本机</span>
          </div>
          <AppUpdateEntry
            state={appUpdate}
            expanded={popup?.kind === "update"}
            onClick={openUpdates}
          />
        </div>
      </aside>
      <div className="workspace">
        {!desktopRuntime && (
          <div className="preview-banner" role="status">
            <Monitor size={15} aria-hidden />
            浏览器预览 · 应用操作仅作演示，不会修改客户端配置
          </div>
        )}
        {extraClient ? (
          <Tabs.Content
            value={extraClient}
            key={extraClient}
            className="extra-client-workspace"
          >
            <ExtraClientPanel
              key={extraClient}
              client={extraClient}
              providers={providers}
              onManage={() => switchFamily("codex")}
              onBusy={setBusy}
              onEdit={(value) =>
                setPopup({ kind: "config-editor", client: value })
              }
              onWritten={(value, result) => {
                setPopup({ kind: "config-restart", client: value, result });
                void client.invalidateQueries({
                  queryKey: ["client-config", value],
                });
              }}
            />
          </Tabs.Content>
        ) : (
          <>
            <header className="workspace-header" data-tauri-drag-region>
              <div className="workspace-title" data-tauri-drag-region>
                <h1 data-tauri-drag-region>{product}</h1>
                <span
                  className="supplier-total"
                  aria-label={`已保存 ${providers.length} 个 API 供应商`}
                >
                  <span className="supplier-total-label">供应商</span>
                  <span className="count">{providers.length}</span>
                </span>
                {family === "claude" && (
                  <div
                    className="segmented"
                    role="group"
                    aria-label="Claude 配置目标"
                  >
                    <button
                      disabled={busy}
                      aria-pressed={target === "claude_desktop"}
                      onClick={() => changeTarget("claude_desktop")}
                    >
                      <Monitor size={16} aria-hidden />
                      桌面端
                    </button>
                    <button
                      disabled={busy}
                      aria-pressed={target === "claude_cli"}
                      onClick={() => changeTarget("claude_cli")}
                    >
                      <Terminal size={16} aria-hidden />
                      CLI
                    </button>
                  </div>
                )}
              </div>
              <div className="header-tools">
                {!!providers.length &&
                  providers.length <= 3 &&
                  !searchExpanded && (
                    <button
                      type="button"
                      className="icon-button"
                      aria-label="搜索供应商"
                      title="搜索供应商（Ctrl+F）"
                      onClick={() => setSearchExpanded(true)}
                    >
                      <Search size={17} aria-hidden />
                    </button>
                  )}
                {providers.length > 0 && (
                  <button
                    className="button primary"
                    data-dialog-fallback
                    disabled={busy || query.isPending}
                    onClick={(e) => open({ kind: "create" }, e)}
                  >
                    <Plus size={17} aria-hidden />
                    添加供应商
                  </button>
                )}
                <button
                  className="icon-button refresh-button"
                  aria-label="刷新配置状态"
                  title="刷新配置状态"
                  disabled={busy || query.isFetching}
                  onClick={() =>
                    void action(async () => {
                      const result = await query.refetch();
                      if (result.error) throw result.error;
                    }, "配置状态已刷新。")
                  }
                >
                  <RefreshCw
                    size={18}
                    className={query.isFetching ? "spinning" : ""}
                    aria-hidden
                  />
                </button>
                <button
                  className="button settings-trigger"
                  aria-label="设置"
                  title="设置"
                  disabled={busy || !status}
                  onClick={(e) => open({ kind: "settings" }, e)}
                >
                  <SlidersHorizontal size={17} aria-hidden />
                  <span>设置</span>
                </button>
              </div>
            </header>
            <main>
              <Tabs.Content value={family} key={family}>
                {notice && (
                  <div
                    className={`notice ${notice.error ? "error" : ""}`}
                    role={notice.error ? "alert" : "status"}
                  >
                    {notice.error ? (
                      <Unplug size={18} aria-hidden />
                    ) : (
                      <Check size={18} aria-hidden />
                    )}
                    <span>{notice.text}</span>
                    {notice.syncProviderId && (
                      <button
                        className="text-button"
                        disabled={busy}
                        onClick={() =>
                          void syncProvider(notice.syncProviderId!)
                        }
                      >
                        同步其他客户端
                      </button>
                    )}
                    {!!notice.details && (
                      <details>
                        <summary>处理问题</summary>
                        <ErrorFeedback
                          error={notice.details}
                          onAction={
                            notice.onAction ||
                            (() =>
                              errorCode(notice.details) === "external_change"
                                ? setPopup({ kind: "conflict" })
                                : setPopup({ kind: "settings" }))
                          }
                        />
                      </details>
                    )}
                    <button
                      aria-label="关闭提示"
                      onClick={() => setNotice(null)}
                    >
                      ×
                    </button>
                  </div>
                )}
                {query.error && query.data && (
                  <div className="error-notice" role="alert">
                    {errorMessage(query.error)}
                    <button
                      className="text-button"
                      onClick={() => void query.refetch()}
                    >
                      重试
                    </button>
                  </div>
                )}
                <div className="workspace-grid">
                  <section
                    className="config-section"
                    aria-labelledby="config-heading"
                  >
                    <h2 id="config-heading" className="sr-only">
                      API 供应商
                    </h2>
                    {(providers.length > 3 || search || searchExpanded) && (
                      <div className="search-field">
                        <Search size={17} aria-hidden />
                        <input
                          aria-label="搜索 API 配置"
                          placeholder="搜索名称、地址或模型"
                          value={search}
                          ref={searchInput}
                          title="Ctrl+F 搜索供应商"
                          onChange={(e) => setSearch(e.target.value)}
                          onKeyDown={(e) => {
                            if (e.key === "Escape" && search) {
                              e.preventDefault();
                              setSearch("");
                            }
                          }}
                        />
                        {search && (
                          <>
                            <span className="search-result-count" role="status">
                              {visible.length} / {providers.length}
                            </span>
                            <button
                              type="button"
                              className="icon-button"
                              aria-label="清空供应商搜索"
                              onClick={() => {
                                setSearch("");
                                searchInput.current?.focus();
                              }}
                            >
                              <X size={16} aria-hidden />
                            </button>
                          </>
                        )}
                      </div>
                    )}
                    {query.isPending ? (
                      <div className="empty-state" role="status">
                        <RefreshCw className="spinning" size={26} aria-hidden />
                        <p>正在读取你的配置…</p>
                      </div>
                    ) : query.error && !query.data ? (
                      <div className="empty-state load-failure" role="alert">
                        <Unplug size={26} aria-hidden />
                        <h3>暂时无法读取供应商</h3>
                        <p>数据尚未读取，请重试后继续操作。</p>
                        <button
                          className="button primary"
                          disabled={query.isFetching}
                          onClick={() => void query.refetch()}
                        >
                          <RefreshCw size={16} aria-hidden />
                          重新读取
                        </button>
                        <details>
                          <summary>查看原因</summary>
                          <p>{errorMessage(query.error)}</p>
                        </details>
                      </div>
                    ) : providers.length === 0 ? (
                      <div className="empty-state">
                        <span className="empty-icon" aria-hidden>
                          <Plus size={26} />
                        </span>
                        <h3>还没有 API 供应商</h3>
                        <p>
                          填写供应商提供的地址和密钥，自动获取模型，
                          <br />
                          填入一次，以后轻松切换。
                        </p>
                        <button
                          className="button primary button-large"
                          disabled={busy}
                          onClick={(e) => open({ kind: "create" }, e)}
                        >
                          <Plus size={18} aria-hidden />
                          添加第一个供应商
                          <ArrowRight size={17} aria-hidden />
                        </button>
                        <span className="empty-note">
                          <ShieldCheck size={15} aria-hidden />
                          应用前自动备份，随时可以恢复
                        </span>
                      </div>
                    ) : (
                      <ul
                        className="provider-list"
                        aria-label="已保存的 API 配置"
                        aria-busy={busy}
                      >
                        {visible.map((provider) => {
                          const isActive =
                            status?.activeProviderId === provider.id;
                          const displayedModel =
                            isActive && status?.appliedModel
                              ? status.appliedModel
                              : provider.model;
                          const codexChoiceDiffers =
                            target === "codex" &&
                            isActive &&
                            status?.state === "applied" &&
                            !!status.configuredModel &&
                            (status.configuredModel !== displayedModel ||
                              (!!provider.reasoningEffort &&
                                status.configuredReasoningEffort !==
                                  provider.reasoningEffort));
                          const upToDate =
                            isActive && applied && !codexChoiceDiffers;
                          const conversionBlocked =
                            needsProtocolConversion(provider, target) &&
                            !protocolConversionEnabled(provider, target);
                          const protocolPending =
                            desktopRuntime && !protocolConfirmed(provider);
                          return (
                            <li
                              key={provider.id}
                              className={`provider-card ${isActive ? "is-active" : ""}`}
                            >
                              <div className="provider-heading">
                                <div className="provider-name">
                                  <div className="provider-title">
                                    <ProviderName
                                      provider={provider}
                                      displayName={displayProviderName(
                                        provider,
                                        providers,
                                      )}
                                      disabled={busy}
                                      editing={renameProviderId === provider.id}
                                      onEditing={(editing) => {
                                        setRenameProviderId(
                                          editing ? provider.id : null,
                                        );
                                        if (!editing)
                                          requestAnimationFrame(() =>
                                            document
                                              .querySelector<HTMLButtonElement>(
                                                `[data-provider-name="${provider.id}"]`,
                                              )
                                              ?.focus(),
                                          );
                                      }}
                                      onSave={async (expected, name) =>
                                        !!(await action(
                                          () => api.rename(expected, name),
                                          "供应商名称已更新。",
                                          false,
                                          provider.id,
                                        ))
                                      }
                                    />
                                    <button
                                      type="button"
                                      className={`provider-pin-button ${listPreferences.pinned.includes(provider.id) ? "is-pinned" : ""}`}
                                      aria-label={`${listPreferences.pinned.includes(provider.id) ? "取消置顶" : "置顶"} ${provider.name}`}
                                      aria-pressed={listPreferences.pinned.includes(
                                        provider.id,
                                      )}
                                      title={
                                        listPreferences.pinned.includes(
                                          provider.id,
                                        )
                                          ? "取消置顶"
                                          : "置顶供应商"
                                      }
                                      onClick={() =>
                                        setListPreferences((p) => ({
                                          ...p,
                                          pinned: p.pinned.includes(provider.id)
                                            ? p.pinned.filter(
                                                (id) => id !== provider.id,
                                              )
                                            : [...p.pinned, provider.id],
                                        }))
                                      }
                                    >
                                      <Pin size={13} aria-hidden />
                                      {listPreferences.pinned.includes(
                                        provider.id,
                                      )
                                        ? "已置顶"
                                        : "置顶"}
                                    </button>
                                    {isActive &&
                                      activeState.tone === "attention" && (
                                        <span
                                          className={`current-label ${activeState.tone === "attention" ? "attention" : ""}`}
                                        >
                                          <span aria-hidden />
                                          {activeState.label}
                                        </span>
                                      )}
                                  </div>
                                  <span
                                    className="provider-address"
                                    title={provider.baseUrl}
                                  >
                                    <span className="provider-address-label">
                                      API 地址
                                    </span>
                                    <span>{provider.baseUrl}</span>
                                  </span>
                                </div>
                              </div>
                              <ProviderProtocolControl
                                provider={provider}
                                target={target}
                                active={isActive}
                                disabled={busy}
                                toggleDisabled={
                                  isActive && status?.state !== "applied"
                                }
                                saving={conversionProviderId === provider.id}
                                onToggle={(provider, enabled) =>
                                  void toggleConversion(provider, enabled)
                                }
                              />
                              <div
                                className={`provider-meta ${target === "codex" ? "is-codex" : "is-claude"}`}
                              >
                                <div className="provider-model-controls">
                                  <ProviderModelSelect
                                    provider={provider}
                                    target={target}
                                    displayedModel={displayedModel}
                                    disabled={
                                      busy ||
                                      (isActive && status?.state !== "applied")
                                    }
                                    expanded={
                                      popup?.kind === "models" &&
                                      popup.provider.id === provider.id
                                    }
                                    onExpand={(event) =>
                                      open(
                                        { kind: "models", provider, target },
                                        event,
                                      )
                                    }
                                    onSave={quickModels}
                                  />
                                  {target === "codex" && (
                                    <div className="provider-setting provider-fast-control">
                                      <span className="provider-setting-label">
                                        Fast 加速模式
                                      </span>
                                      <label
                                        className="provider-fast-toggle"
                                        title={
                                          providerProtocol(provider) ===
                                          "anthropic"
                                            ? "Claude 协议转换暂不支持 Fast。"
                                            : "加速模式需供应商支持，可能增加费用。"
                                        }
                                      >
                                        <input
                                          type="checkbox"
                                          role="switch"
                                          aria-label={`Fast 模式 · ${provider.name}`}
                                          aria-describedby={`fast-hint-${provider.id}`}
                                          checked={
                                            provider.codexOptions?.fastMode ===
                                            true
                                          }
                                          disabled={
                                            busy ||
                                            providerProtocol(provider) ===
                                              "anthropic" ||
                                            (isActive &&
                                              (status?.state ===
                                                "external_change" ||
                                                status?.state === "error"))
                                          }
                                          onChange={() =>
                                            void toggleFast(provider)
                                          }
                                        />
                                        <span
                                          className="provider-fast-track"
                                          aria-hidden
                                        />
                                        <span>
                                          {fastProviderId === provider.id
                                            ? "保存中…"
                                            : provider.codexOptions?.fastMode
                                              ? "已开启"
                                              : "已关闭"}
                                        </span>
                                      </label>
                                      <span
                                        id={`fast-hint-${provider.id}`}
                                        className="provider-setting-help"
                                      >
                                        {providerProtocol(provider) ===
                                        "anthropic"
                                          ? "Claude 转换暂不支持"
                                          : "需供应商支持，可能增加费用"}
                                      </span>
                                    </div>
                                  )}
                                </div>
                                <ProviderBalance
                                  key={`${provider.id}-${provider.updatedAt}-${provider.keySuffix}-${provider.baseUrl}-${JSON.stringify(provider.codexOptions?.balanceQuery)}`}
                                  provider={provider}
                                />
                              </div>
                              {codexChoiceDiffers && (
                                <p
                                  className="provider-state-note attention"
                                  role="status"
                                >
                                  Codex 配置当前选择：
                                  <code>{status.configuredModel}</code>
                                  {" · 思考强度 "}
                                  <code>
                                    {status.configuredReasoningEffort ||
                                      "模型默认"}
                                  </code>
                                  。点击「重新应用」恢复供应商默认模型，
                                  {provider.reasoningEffort
                                    ? `思考强度使用供应商设置 ${provider.reasoningEffort}。`
                                    : "并保留此思考强度。"}
                                  已有会话请在 Codex 内确认模型和思考强度。
                                </p>
                              )}
                              {isActive && activeState.tone === "attention" && (
                                <p
                                  className={
                                    "provider-state-note " + activeState.tone
                                  }
                                  role="status"
                                >
                                  {activeState.note}
                                  {needsRestart(runtime.data) &&
                                    runtime.data && (
                                      <button
                                        type="button"
                                        className="text-button"
                                        disabled={busy}
                                        onClick={(e) =>
                                          open(
                                            {
                                              kind: "restart-client",
                                              runtime: runtime.data!,
                                            },
                                            e,
                                          )
                                        }
                                      >
                                        重启 {restartNames[target]}
                                      </button>
                                    )}
                                  {(status?.state === "external_change" ||
                                    status?.state === "error") && (
                                    <button
                                      className="text-button"
                                      onClick={() =>
                                        setPopup({ kind: "conflict" })
                                      }
                                    >
                                      查看配置
                                    </button>
                                  )}
                                </p>
                              )}
                              <div className="provider-actions">
                                <button
                                  className={`button apply-button ${upToDate ? "applied-button" : "secondary"}`}
                                  disabled={
                                    busy ||
                                    conversionBlocked ||
                                    protocolPending ||
                                    upToDate ||
                                    status?.state === "error"
                                  }
                                  aria-describedby={
                                    conversionBlocked || protocolPending
                                      ? `protocol-hint-${provider.id}`
                                      : undefined
                                  }
                                  onClick={(event) => {
                                    document
                                      .querySelector("[data-dialog-return]")
                                      ?.removeAttribute("data-dialog-return");
                                    event.currentTarget.setAttribute(
                                      "data-dialog-return",
                                      "",
                                    );
                                    void useProvider(provider, target);
                                  }}
                                >
                                  {busyProviderId === provider.id &&
                                  fastProviderId !== provider.id
                                    ? quickProviderId === provider.id ||
                                      conversionProviderId === provider.id
                                      ? "保存中…"
                                      : "正在应用…"
                                    : codexChoiceDiffers
                                      ? "重新应用"
                                      : upToDate
                                        ? activeState.label
                                        : isActive &&
                                            status?.state === "saved_changes"
                                          ? "更新配置"
                                          : "使用"}
                                  {upToDate ? (
                                    <Check size={15} aria-hidden />
                                  ) : (
                                    <ArrowRight size={15} aria-hidden />
                                  )}
                                </button>
                                <button
                                  type="button"
                                  className="provider-edit-button"
                                  aria-label={`编辑 ${provider.name}`}
                                  disabled={busy}
                                  onClick={(e) =>
                                    open({ kind: "edit", provider }, e)
                                  }
                                >
                                  <Pencil size={14} aria-hidden />
                                  编辑
                                </button>
                                <button
                                  type="button"
                                  className="provider-delete-button"
                                  title={
                                    query.data?.targets.some(
                                      (t) => t.activeProviderId === provider.id,
                                    )
                                      ? "此供应商正在使用，请先切换或恢复"
                                      : "删除配置"
                                  }
                                  aria-label={`删除 ${provider.name}`}
                                  disabled={
                                    busy ||
                                    query.data?.targets.some(
                                      (t) => t.activeProviderId === provider.id,
                                    )
                                  }
                                  onClick={(e) =>
                                    open({ kind: "delete", provider }, e)
                                  }
                                >
                                  <Trash2 size={14} aria-hidden />
                                  删除
                                </button>
                              </div>
                              {query.data?.targets.some(
                                (t) =>
                                  t.target !== target &&
                                  t.activeProviderId === provider.id &&
                                  t.state === "saved_changes",
                              ) && (
                                <div className="provider-sync-row">
                                  <span>其他客户端的连接配置有待更新</span>
                                  <button
                                    className="text-button provider-sync-button"
                                    disabled={busy}
                                    title="同步地址与密钥，保留各客户端的模型和上下文"
                                    onClick={() =>
                                      void syncProvider(provider.id)
                                    }
                                  >
                                    同步其他客户端
                                  </button>
                                </div>
                              )}
                            </li>
                          );
                        })}
                      </ul>
                    )}
                    {providers.length > 0 && visible.length === 0 && (
                      <div className="no-results">
                        <Search size={22} aria-hidden />
                        <p>没有找到匹配的配置</p>
                        <button
                          className="text-button"
                          onClick={() => setSearch("")}
                        >
                          清空搜索
                        </button>
                      </div>
                    )}
                    <div className="section-footnote">
                      <ShieldCheck size={15} aria-hidden />
                      <span>保留客户端的其他设置，仅修改 API 配置。</span>
                    </div>
                  </section>
                </div>
              </Tabs.Content>
            </main>
            <footer className="workspace-footer">
              <div className="footer-status">
                <span className="footer-status-text">
                  <span className="footer-dot" />
                  {runtime.data?.bridgeRequired && runtime.data.bridgeHealthy
                    ? "兼容服务运行中 · 关闭窗口后保留托盘"
                    : desktopRuntime
                      ? "配置保存在本机"
                      : "界面预览"}
                </span>
                {runtime.data?.bridgeRequired &&
                  background.data?.supported &&
                  !background.data.enabled && (
                    <button
                      className="text-button"
                      disabled={busy}
                      onClick={() =>
                        void action(async () => {
                          await api.setBackground(true);
                          await background.refetch();
                        }, "已开启后台启动。下次登录 Windows 时自动恢复兼容连接。")
                      }
                    >
                      开启后台启动
                    </button>
                  )}
              </div>
              <ServicePromotion />
            </footer>
          </>
        )}
      </div>
      {popup?.kind === "config-editor" && (
        <ClientConfigEditor
          key={popup.client}
          client={popup.client}
          onClose={() => setPopup(null)}
          onSaved={(result) => {
            const configClient = popup.client;
            if (
              ["codex", "claude_desktop", "claude_cli"].includes(configClient)
            )
              promptedRevision.current[configClient as Target] =
                result.configurationRevision;
            setPopup({ kind: "config-restart", client: configClient, result });
            void client.invalidateQueries({ queryKey: ["overview"] });
            void client.invalidateQueries({
              queryKey: ["client-config", configClient],
            });
            void client.invalidateQueries({ queryKey: ["runtime"] });
          }}
        />
      )}
      {popup?.kind === "config-restart" && (
        <ConfigRestartNotice
          client={popup.client}
          result={popup.result}
          onClose={() => setPopup(null)}
        />
      )}
      {popup?.kind === "update" && (
        <Modal
          title="软件更新"
          description="每次更新选择 GitHub 手动下载或远程更新。"
          busy={updateBusy}
          dismissOnOutside={!updateBusy}
          onClose={() => setPopup(null)}
        >
          <AppUpdatePanel
            state={appUpdate}
            showHeading={false}
            onBusy={setUpdateBusy}
          />
        </Modal>
      )}
      {popup?.kind === "models" && (
        <ProviderModelsDialog
          key={`${popup.target}-${popup.provider.id}`}
          provider={popup.provider}
          target={popup.target}
          disabled={
            busy ||
            (status?.activeProviderId === popup.provider.id &&
              status.state !== "applied")
          }
          applied={status?.activeProviderId === popup.provider.id}
          writing={busy}
          saveError={notice?.error ? notice.text : ""}
          onClose={() => setPopup(null)}
          onSave={quickModels}
        />
      )}
      {(popup?.kind === "create" || popup?.kind === "edit") && (
        <ProviderForm
          family={family}
          target={target}
          provider={popup.kind === "edit" ? popup.provider : undefined}
          startWithModels={popup.kind === "edit" && !!popup.models}
          applyOnSave={
            popup.kind === "create" ||
            status?.activeProviderId === popup.provider.id
          }
          onClose={() => setPopup(null)}
          onCommit={async (input, apply) => {
            const result = await api.commit(input, target, apply);
            const failures: string[] = [];
            if (listPreferences.autoSync && input.id && apply) {
              try {
                const synced = await api.syncTargets(result.id);
                failures.push(
                  ...synced
                    .filter((r) => !r.success)
                    .map(
                      (r) =>
                        targetNames[r.target] +
                        "：" +
                        explainError(r.error).message,
                    ),
                );
              } catch (error) {
                failures.push(explainError(error).message);
              }
            }
            let latest = query.data;
            try {
              latest = await api.overview();
            } catch {
              /* A completed commit stays successful even if refreshing the view fails. */
            }
            commitResult.current = {
              provider: result,
              pending: !!latest?.targets.some(
                (t) =>
                  t.activeProviderId === result.id && t.state !== "applied",
              ),
              failures,
            };
            setListPreferences((p) => ({
              ...p,
              recent: apply
                ? { ...p.recent, [result.id]: Date.now() }
                : p.recent,
            }));
            await client.invalidateQueries({ queryKey: ["overview"] });
            await client.invalidateQueries({ queryKey: ["runtime"] });
            return result;
          }}
          onComplete={(didApply) =>
            setNotice({
              error: !!commitResult.current?.failures.length,
              text:
                (commitResult.current?.provider.reusedExisting
                  ? "已复用已有供应商。"
                  : "") +
                (didApply
                  ? desktopRuntime
                    ? `配置已写入 ${targetNames[target]}。请按供应商行的提示加载配置。`
                    : "预览：已保存并选择配置，未修改客户端文件。"
                  : "供应商已保存。两个客户端均可从列表中使用。") +
                (commitResult.current?.pending
                  ? " 其他已使用客户端有待更新。"
                  : "") +
                (commitResult.current?.failures.length
                  ? " " + commitResult.current.failures.join("；")
                  : ""),
              syncProviderId: commitResult.current?.pending
                ? commitResult.current.provider.id
                : undefined,
            })
          }
        />
      )}
      {popup?.kind === "delete" && (
        <Modal
          title="删除这组配置？"
          description={`「${popup.provider.name}」将从列表中移除。正在使用的配置需要先切换或恢复。`}
          onClose={() => setPopup(null)}
          busy={busy}
        >
          <div className="modal-actions">
            <button
              className="button secondary"
              disabled={busy}
              onClick={() => setPopup(null)}
            >
              取消
            </button>
            <button
              className="button danger"
              disabled={
                busy ||
                query.data?.targets.some(
                  (t) => t.activeProviderId === popup.provider.id,
                )
              }
              onClick={() =>
                void action(
                  () => api.delete(popup.provider.id),
                  "配置已删除。",
                  true,
                )
              }
            >
              删除配置
            </button>
          </div>
          {notice?.error && (
            <p className="error-notice" role="alert">
              {notice.text}
            </p>
          )}
        </Modal>
      )}
      {popup?.kind === "restart-client" && (
        <ClientRestartDialog
          key={`${popup.runtime.target}:${popup.runtime.configurationRevision}`}
          runtime={popup.runtime}
          onLater={() => setPopup(null)}
          onRestarted={(message) => {
            setPopup(null);
            setNotice({ error: false, text: message });
            void client.invalidateQueries({ queryKey: ["runtime"] });
          }}
        />
      )}
      {popup?.kind === "restore" && (
        <Modal
          title="恢复原来的 API 配置？"
          description={`将 ${targetNames[target]} 的 API 设置恢复到 uni-switch 接管前的状态，保留其他设置。完成后需重启客户端。`}
          onClose={() => setPopup(null)}
          busy={busy}
        >
          <div className="modal-actions">
            <button
              className="button secondary"
              disabled={busy}
              onClick={() => setPopup(null)}
            >
              取消
            </button>
            <button
              className="button primary"
              disabled={busy}
              onClick={() =>
                void action(
                  () => api.restore(target),
                  "已恢复原 API 配置，请重新启动客户端。",
                  true,
                )
              }
            >
              <RotateCcw size={16} aria-hidden />
              恢复原配置
            </button>
          </div>
          {notice?.error && (
            <p className="error-notice" role="alert">
              {notice.text}
            </p>
          )}
        </Modal>
      )}
      {popup?.kind === "settings" && (
        <Modal
          title={`${product} 设置`}
          description={`设置${targetNames[target]}的配置位置，或恢复原来的 API 配置。`}
          wide
          onClose={() => setPopup(null)}
          busy={busy}
        >
          <section
            className="settings-section"
            aria-labelledby="location-heading"
          >
            <h3 id="location-heading">配置位置</h3>
            <button
              className="button secondary"
              disabled={busy}
              onClick={() =>
                setPopup({ kind: "config-editor", client: target })
              }
            >
              查看 / 编辑配置文件
            </button>
            <code className="settings-path">{status?.directory}</code>
            <p id="location-hint">
              直接使用此目录读取和写入配置。其他位置的配置不会影响当前选择。
            </p>
            <button
              className="button secondary"
              disabled={busy || status?.canRestore}
              aria-describedby="location-hint location-managed-hint"
              onClick={() => {
                setDirectory(status?.directory || "");
                setNotice(null);
                setPopup({ kind: "directory" });
              }}
            >
              <Pencil size={16} aria-hidden />
              修改目录
            </button>
            <p id="location-managed-hint">
              {status?.canRestore
                ? "当前目录已接管，更换前请先恢复原配置。"
                : "通常无需修改；使用自定义位置时手动指定即可。"}
            </p>
            {!!status?.files.length && (
              <details className="files-details">
                <summary>查看配置文件</summary>
                <ul>
                  {status.files.map((file) => (
                    <li key={file}>
                      <code>{file}</code>
                    </li>
                  ))}
                </ul>
              </details>
            )}
            {notice && (
              <p
                className={notice.error ? "error-notice" : "settings-result"}
                role={notice.error ? "alert" : "status"}
              >
                {notice.text}
              </p>
            )}
          </section>
          <section
            className="settings-section"
            aria-labelledby="background-heading"
          >
            <h3 id="background-heading">后台运行</h3>
            <label className="settings-toggle">
              <input
                type="checkbox"
                checked={background.data?.enabled || false}
                disabled={busy || !background.data?.supported}
                onChange={(e) =>
                  void action(async () => {
                    await api.setBackground(e.target.checked);
                    await background.refetch();
                  }, "后台启动设置已保存。")
                }
              />
              随 Windows 启动，在后台运行
            </label>
            <p>
              电脑重启后自动恢复兼容连接，不弹出主窗口。关闭主窗口会保留托盘；从托盘退出会停止兼容连接。
            </p>
            {!background.data?.supported && (
              <small>隔离预览或当前平台不修改系统启动项。</small>
            )}
            {!!background.error && (
              <ErrorFeedback
                error={background.error}
                onAction={() => void background.refetch()}
              />
            )}
          </section>
          <ModelRegistryPanel />
          <section className="settings-section" aria-labelledby="sync-heading">
            <h3 id="sync-heading">供应商更新</h3>
            <label className="settings-toggle">
              <input
                type="checkbox"
                checked={listPreferences.autoSync}
                onChange={(e) =>
                  setListPreferences((p) => ({
                    ...p,
                    autoSync: e.target.checked,
                  }))
                }
              />
              保存供应商后，自动更新所有已使用它的客户端
            </label>
            <p>
              同步地址与密钥，保留各客户端的默认模型、上下文和其他专属设置。遇到冲突时保留该客户端原配置并提示。
            </p>
          </section>
          <section
            className="settings-section"
            aria-labelledby="license-heading"
          >
            <h3 id="license-heading">开源许可</h3>
            <p>uni-switch · AGPL-3.0-only</p>
            <p>Copyright © 2026 dieqiyun and uni-switch contributors.</p>
            <p>
              本软件按现状提供，不提供任何保证。你可以按 AGPL 使用、修改和分发；
              分发修改版或通过网络提供修改版服务时，请按协议提供对应源码。
            </p>
            <div className="project-links">
              {(["source", "license"] as const).map((page) => (
                <a
                  key={page}
                  className="button secondary"
                  href={
                    page === "source"
                      ? "https://github.com/dieqiyun/uni-switch"
                      : "https://github.com/dieqiyun/uni-switch/blob/main/LICENSE"
                  }
                  target="_blank"
                  rel="noopener noreferrer"
                  onClick={(event) => {
                    if (!desktopRuntime) return;
                    event.preventDefault();
                    void action(
                      () => api.openProjectPage(page),
                      "已打开项目页面。",
                    );
                  }}
                >
                  {page === "source" ? "查看源码 ↗" : "完整许可 ↗"}
                </a>
              ))}
            </div>
          </section>
          <section
            className="settings-section"
            aria-labelledby="restore-heading"
          >
            <h3 id="restore-heading">恢复 API 配置</h3>
            <p id="restore-hint">
              {status?.canRestore
                ? "恢复到 uni-switch 接管前的 API 设置，保留其他配置。"
                : "应用第一组配置后，可以在这里恢复原来的 API 设置。"}
            </p>
            <button
              className="button secondary"
              disabled={!status?.canRestore || busy}
              aria-describedby="restore-hint"
              onClick={() => setPopup({ kind: "restore" })}
            >
              <RotateCcw size={16} aria-hidden />
              恢复原配置
            </button>
          </section>
        </Modal>
      )}
      {popup?.kind === "directory" && (
        <Modal
          title="设置配置位置"
          description={
            target === "claude_desktop"
              ? "填写包含 Claude 和 Claude-3p 的父目录：Windows 通常是 LocalAppData；macOS 是 ~/Library/Application Support；Linux 是 ~/.config。"
              : "填写客户端实际使用的配置目录。使用默认目录时，无需修改。"
          }
          onClose={() => {
            setNotice(null);
            setPopup({ kind: "settings" });
          }}
          busy={busy}
        >
          <form
            onSubmit={(e) => {
              e.preventDefault();
              void action(async () => {
                await api.directory(target, directory);
                setPopup({ kind: "settings" });
              }, "配置目录已更新。");
            }}
          >
            <div className="field">
              <label htmlFor="directory">配置目录</label>
              <input
                id="directory"
                value={directory}
                onChange={(e) => setDirectory(e.target.value)}
                autoFocus
                required
                spellCheck={false}
                disabled={busy}
                aria-describedby="directory-hint"
              />
              <small id="directory-hint">
                填写绝对路径。已应用的目录需要先恢复原配置，才能更换。
              </small>
            </div>
            {status?.files.length ? (
              <details className="files-details">
                <summary>查看将修改的文件</summary>
                <ul>
                  {status.files.map((file) => (
                    <li key={file}>
                      <code>{file}</code>
                    </li>
                  ))}
                </ul>
              </details>
            ) : null}
            <div className="modal-actions">
              <button
                className="button secondary"
                type="button"
                disabled={busy}
                onClick={() => {
                  setNotice(null);
                  setPopup({ kind: "settings" });
                }}
              >
                取消
              </button>
              <button className="button primary" type="submit" disabled={busy}>
                保存目录
              </button>
            </div>
            {notice?.error && (
              <p className="error-notice" role="alert">
                {notice.text}
              </p>
            )}
          </form>
        </Modal>
      )}
      {popup?.kind === "overwrite" && (
        <Modal
          title="覆盖现有 API 配置？"
          description={`${targetNames[popup.confirmation.target]} 的 API 配置已被其他工具修改，是否强制覆盖并使用「${popup.provider.name}」？`}
          onClose={() => {
            setNotice(null);
            setPopup(null);
          }}
          busy={busy}
          dismissOnOutside={false}
          initialFocusId="cancel-overwrite"
        >
          <p>
            只覆盖此客户端的
            API、模型和相关受管设置，保留其他设置。覆盖前会自动备份当前文件，其他客户端不受影响。
          </p>
          <p className="scope-hint">
            若其他切换工具仍在自动写入，请先暂停它，避免配置反复被改动。
          </p>
          <p>配置目录</p>
          <code className="settings-path">{popup.confirmation.directory}</code>
          {popup.confirmation.files.length > 0 && (
            <details className="files-details">
              <summary>
                将更新 {popup.confirmation.files.length} 个配置文件
              </summary>
              <ul>
                {popup.confirmation.files.map((file) => (
                  <li key={file}>
                    <code>{file}</code>
                  </li>
                ))}
              </ul>
            </details>
          )}
          {notice?.error && (
            <p className="error-notice" role="alert">
              {notice.text}
            </p>
          )}
          <div className="modal-actions">
            <button
              id="cancel-overwrite"
              className="button secondary"
              disabled={busy}
              onClick={() => {
                setNotice(null);
                setPopup(null);
              }}
            >
              取消
            </button>
            <button
              className="button danger"
              disabled={busy}
              onClick={() =>
                void useProvider(
                  popup.provider,
                  popup.confirmation.target,
                  popup.confirmation,
                )
              }
            >
              {busy ? "正在覆盖…" : "强制覆盖并使用"}
            </button>
          </div>
        </Modal>
      )}
      {popup?.kind === "conflict" && (
        <Modal
          title="处理现有配置"
          description="为保护现有配置，软件已暂停写入。可关闭此窗口并点击供应商的「使用」确认覆盖，或先撤回其他工具的修改。"
          wide
          onClose={() => setPopup(null)}
          busy={busy}
        >
          <p className="scope-hint">{status?.message}</p>
          <p>当前目标：{targetNames[target]}</p>
          <code className="settings-path">{status?.directory}</code>
          <details className="files-details">
            <summary>涉及的配置文件</summary>
            <ul>
              {status?.files.map((file) => (
                <li key={file}>
                  <code>{file}</code>
                </li>
              ))}
            </ul>
          </details>
          <div className="modal-actions">
            <button
              className="button secondary"
              onClick={() =>
                setPopup({ kind: "help", topic: "troubleshooting" })
              }
            >
              查看处理教程
            </button>
            <button className="button primary" onClick={() => setPopup(null)}>
              知道了
            </button>
          </div>
          {notice?.error && (
            <p className="error-notice" role="alert">
              {notice.text}
            </p>
          )}
        </Modal>
      )}
      {popup?.kind === "help" && (
        <TutorialDialog
          initialTopic={popup.topic}
          onClose={() => setPopup(null)}
        />
      )}
    </Tabs.Root>
  );
}
