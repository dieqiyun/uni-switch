import { invoke, isTauri } from "@tauri-apps/api/core";
import { invalidateBalance } from "./balanceCache";
import type {
  Overview,
  Provider,
  ProviderInput,
  Target,
  TargetStatus,
  BalanceQuery,
  BalanceResult,
  ConnectionInput,
  ModelSyncResult,
  ReasoningRepairResult,
  ModelWriteInput,
  RuntimeStatus,
  BackgroundSettings,
  SyncTargetResult,
  FastModeResult,
  RestartResult,
  QuickModelInput,
  UpdateSource,
  UpdateCheck,
  UpdateDownloadStatus,
  UpdateInstallResult,
  ProtocolConversionResult,
  ApplyOverwriteConfirmation,
  ModelRegistryStatus,
  ModelRegistry,
  ModelVerification,
} from "../types";
import { APP_VERSION } from "./appVersion";
import releaseConfig from "../../release-config.json";
import registry from "../content/model-capabilities.json";
import {
  needsProtocolConversion,
  protocolConversionEnabled,
} from "./protocolConversion";

export const desktopRuntime = isTauri();
const previewKey = "uni-switch-preview-v1";
const emptyPreview = (): Overview => ({
  providers: [],
  dataDirectory: "浏览器预览 · 数据仅保存在此浏览器",
  targets: (["codex", "claude_desktop", "claude_cli"] as Target[]).map(
    (target) => ({
      target,
      directory:
        target === "codex"
          ? "%USERPROFILE%\\.codex"
          : target === "claude_cli"
            ? "%USERPROFILE%\\.claude"
            : "%LOCALAPPDATA%",
      files: [],
      activeProviderId: null,
      state: "unmanaged",
      canRestore: false,
      message: "在桌面应用中可以将配置写入客户端。",
    }),
  ),
});
function preview(): Overview {
  try {
    return (
      JSON.parse(localStorage.getItem(previewKey) || "null") || emptyPreview()
    );
  } catch {
    return emptyPreview();
  }
}
function persist(data: Overview) {
  localStorage.setItem(previewKey, JSON.stringify(data));
}

export function errorMessage(error: unknown): string {
  if (error && typeof error === "object" && "message" in error)
    return String(error.message);
  return typeof error === "string" ? error : "操作未完成，请刷新后重试。";
}

export const api = {
  modelRegistry: async (): Promise<ModelRegistryStatus> =>
    desktopRuntime
      ? invoke("get_model_registry")
      : {
          registry: registry as ModelRegistry,
          checkedAt: null,
          updated: false,
          updateAvailable: false,
          message: "浏览器预览使用内置资料",
        },
  updateModelRegistry: async (): Promise<ModelRegistryStatus> => {
    if (!desktopRuntime) throw new Error("请在桌面应用中更新模型资料。");
    return invoke("update_model_registry");
  },
  verifyModel: async (
    input: ConnectionInput,
    model: string,
    authMode: Provider["authMode"],
    endpoint: ModelVerification["endpoint"],
    feature: ModelVerification["feature"],
    consent: boolean,
  ): Promise<ModelVerification> => {
    if (!desktopRuntime) throw new Error("浏览器预览不发送推理验证请求。");
    if (!consent) throw new Error("请先确认验证可能计费。");
    return invoke("verify_model_connection", {
      input,
      model,
      authMode,
      endpoint,
      feature,
      consent,
    });
  },
  openProjectPage: async (
    page: "source" | "license" | "tutorial",
  ): Promise<void> => {
    if (desktopRuntime) return invoke("open_project_page", { page });
  },
  openServiceWebsite: async (): Promise<void> => {
    if (desktopRuntime) return invoke("open_service_website");
  },
  detectProtocol: async (expected: Provider): Promise<Provider> => {
    if (!desktopRuntime) throw new Error("请在桌面应用中检测 API 协议。");
    return invoke("detect_provider_protocol", { expected });
  },
  setProtocolConversion: async (
    expected: Provider,
    target: Target,
    enabled: boolean,
  ): Promise<ProtocolConversionResult> => {
    if (desktopRuntime)
      return invoke("set_protocol_conversion", { expected, target, enabled });
    const data = preview();
    const saved = data.providers.find((p) => p.id === expected.id);
    if (!saved || JSON.stringify(saved) !== JSON.stringify(expected))
      throw new Error("供应商已更新，请刷新后重新设置。");
    if (!needsProtocolConversion(saved, target))
      throw new Error("此协议可以直接接入，无需转换。");
    if (protocolConversionEnabled(saved, target) === enabled)
      return { provider: saved, restored: false };
    const disabled = (
      saved.codexOptions?.conversionDisabledTargets || []
    ).filter((t) => t !== target);
    if (!enabled) disabled.push(target);
    saved.codexOptions = {
      fastMode: null,
      contextWindow: null,
      autoCompactTokenLimit: null,
      models: [],
      modelsSyncedAt: null,
      balanceQuery: null,
      ...saved.codexOptions,
      conversionDisabledTargets: disabled,
    };
    saved.updatedAt = Date.now() / 1000;
    const status = data.targets.find(
      (t) => t.target === target && t.activeProviderId === saved.id,
    );
    const restored = !!status && !enabled;
    if (restored) {
      status.activeProviderId = null;
      status.state = "unmanaged";
      status.canRestore = false;
    }
    persist(data);
    return { provider: saved, restored };
  },
  updateSource: async (): Promise<UpdateSource> =>
    desktopRuntime
      ? invoke("get_update_source")
      : {
          currentVersion: APP_VERSION,
          repository: releaseConfig.githubRepository,
        },
  checkUpdate: async (): Promise<UpdateCheck> => {
    if (!desktopRuntime)
      throw new Error("请在 uni-switch 桌面应用中检测更新。");
    return invoke("check_app_update");
  },
  startUpdateDownload: async (
    version: string,
  ): Promise<UpdateDownloadStatus> => {
    if (!desktopRuntime) throw new Error("请在桌面应用中下载更新。");
    return invoke("start_app_update_download", { version });
  },
  updateDownloadStatus: async (): Promise<UpdateDownloadStatus | null> => {
    if (!desktopRuntime) return null;
    return invoke("get_app_update_download");
  },
  cancelUpdateDownload: async (id: string): Promise<void> =>
    invoke("cancel_app_update_download", { id }),
  installUpdate: async (id: string): Promise<UpdateInstallResult> =>
    invoke("install_app_update", { id }),
  openRelease: async (url: string): Promise<void> => {
    if (!desktopRuntime) throw new Error("请在桌面应用中打开 GitHub 发布页。");
    return invoke("open_app_release", { url });
  },
  rename: async (expected: Provider, name: string): Promise<Provider> => {
    if (desktopRuntime) return invoke("rename_provider", { expected, name });
    const data = preview();
    const provider = data.providers.find((p) => p.id === expected.id);
    if (!provider || JSON.stringify(provider) !== JSON.stringify(expected))
      throw new Error("供应商已更新，请刷新后重试。");
    provider.name = name.trim();
    provider.updatedAt = Date.now() / 1000;
    persist(data);
    return provider;
  },
  quickModels: async (
    input: QuickModelInput,
  ): Promise<ReasoningRepairResult> => {
    if (desktopRuntime) return invoke("quick_model_settings", { input });
    const data = preview();
    const provider = data.providers.find((p) => p.id === input.expected.id);
    if (
      !provider ||
      JSON.stringify(provider) !== JSON.stringify(input.expected)
    )
      throw new Error("供应商已更新，请刷新列表后重试。");
    const active = data.targets.find(
      (t) => t.target === input.target && t.activeProviderId === provider.id,
    );
    if (active && active.state !== "applied")
      throw new Error("请先处理待应用的修改或配置冲突。");
    if (!input.models.some((m) => m.enabled && m.id === input.model))
      throw new Error("请选择一个已启用的默认模型。");
    provider.model = input.model;
    provider.codexOptions = {
      fastMode: null,
      contextWindow: null,
      autoCompactTokenLimit: null,
      modelsSyncedAt: null,
      balanceQuery: null,
      ...provider.codexOptions,
      models: input.models,
      ...(input.syncedAt != null ? { modelsSyncedAt: input.syncedAt } : {}),
      ...(input.repairReasoningLevels != null
        ? { repairReasoningLevels: input.repairReasoningLevels }
        : {}),
    };
    provider.updatedAt = Date.now() / 1000;
    for (const t of data.targets) {
      if (t.activeProviderId !== provider.id) continue;
      if (t === active) {
        t.appliedModel = provider.model;
      } else {
        t.state = "saved_changes";
      }
    }
    persist(data);
    return { provider, applied: !!active };
  },
  restartClient: async (
    target: Target,
    configurationRevision: number,
  ): Promise<RestartResult> => {
    if (!desktopRuntime)
      throw new Error("请在 uni-switch 桌面应用中重启客户端。");
    return invoke("restart_client", { target, configurationRevision });
  },
  restartCodex: async (
    configurationRevision: number,
  ): Promise<RestartResult> => {
    if (!desktopRuntime) throw new Error("请在桌面应用中重启 Codex。");
    return invoke("restart_codex_desktop", { configurationRevision });
  },
  setFastMode: async (
    providerId: string,
    enabled: boolean,
  ): Promise<FastModeResult> => {
    if (desktopRuntime)
      return invoke("set_provider_fast_mode", { providerId, enabled });
    const data = preview();
    const provider = data.providers.find((p) => p.id === providerId);
    if (!provider) throw new Error("未找到此供应商，请刷新列表。");
    const upstream =
      provider.codexOptions?.upstreamProtocol ||
      (provider.family === "codex"
        ? provider.codexOptions?.protocol || "openai"
        : provider.codexOptions?.claudeProtocol || "anthropic");
    if (enabled && upstream === "anthropic")
      throw new Error("Claude 协议转换暂不支持 Fast。");
    provider.codexOptions = {
      contextWindow: null,
      autoCompactTokenLimit: null,
      models: [],
      modelsSyncedAt: null,
      balanceQuery: null,
      ...provider.codexOptions,
      fastMode: enabled,
    };
    provider.updatedAt = Date.now() / 1000;
    const applied = data.targets.some(
      (t) => t.target === "codex" && t.activeProviderId === providerId,
    );
    persist(data);
    return { provider, applied };
  },
  background: async (): Promise<BackgroundSettings> =>
    desktopRuntime
      ? invoke("get_background_settings")
      : { supported: false, enabled: false },
  setBackground: async (enabled: boolean): Promise<BackgroundSettings> =>
    invoke("set_background_start", { enabled }),
  syncTargets: async (providerId: string): Promise<SyncTargetResult[]> => {
    if (desktopRuntime) return invoke("sync_provider_targets", { providerId });
    const targets = preview().targets.filter(
      (t) => t.activeProviderId === providerId && t.state !== "applied",
    );
    for (const t of targets) await api.apply(t.target, providerId);
    return targets.map((t) => ({ target: t.target, success: true }));
  },
  runtime: async (target: Target): Promise<RuntimeStatus> =>
    desktopRuntime
      ? invoke("get_runtime_status", { target })
      : {
          target,
          clientRunning: false,
          restartRequired: false,
          bridgeRequired: false,
          bridgeHealthy: true,
        },
  discoverConnection: async (
    input: ConnectionInput,
    protocol: "openai" | "anthropic" | null = null,
    authMode: "bearer" | "x-api-key" | null = null,
  ): Promise<ModelSyncResult> => {
    if (!desktopRuntime) throw new Error("请在桌面应用中自动匹配供应商。");
    return invoke("discover_provider_connection", {
      input,
      protocol,
      authMode,
    });
  },
  commit: async (
    input: ProviderInput,
    target: Target,
    apply: boolean,
  ): Promise<Provider> => {
    if (desktopRuntime) {
      const result = await invoke<Provider>("commit_provider", {
        input,
        target,
        apply,
      });
      invalidateBalance(result.id);
      return result;
    }
    const provider = await api.save(input);
    if (apply) await api.apply(target, provider.id);
    return provider;
  },
  writeModels: async (
    input: ModelWriteInput,
  ): Promise<ReasoningRepairResult> => {
    if (!desktopRuntime) throw new Error("请在桌面应用中写入 Codex 模型。");
    return invoke("update_provider_models", { input });
  },
  repairReasoningLevels: async (
    providerId: string,
  ): Promise<ReasoningRepairResult> => {
    if (!desktopRuntime)
      throw new Error("请在桌面应用中修复 Codex 思考强度列表。");
    return invoke("repair_reasoning_levels", { providerId });
  },
  syncModels: async (
    input: ConnectionInput,
    authMode: Provider["authMode"] = "bearer",
    protocol?: "openai" | "anthropic",
  ): Promise<ModelSyncResult> => {
    if (!desktopRuntime) throw new Error("请在桌面应用中同步供应商模型。");
    return invoke("sync_provider_models", {
      input,
      authMode,
      protocol: protocol || null,
    });
  },
  balance: async (
    input: ConnectionInput,
    query?: BalanceQuery | null,
  ): Promise<BalanceResult> => {
    if (!desktopRuntime) throw new Error("请在桌面应用中查询供应商余额。");
    return invoke("query_provider_balance", { input, query: query || null });
  },
  overview: async (): Promise<Overview> =>
    desktopRuntime ? invoke("get_overview") : preview(),
  save: async (input: ProviderInput): Promise<Provider> => {
    if (desktopRuntime) {
      const provider = await invoke<Provider>("save_provider", { input });
      invalidateBalance(provider.id);
      return provider;
    }
    const data = preview();
    const old = data.providers.find((p) => p.id === input.id);
    if (!old && !input.apiKey?.trim()) throw new Error("请填写 API Key");
    const provider: Provider = {
      id: input.id || crypto.randomUUID(),
      family: input.family,
      name: input.name.trim(),
      baseUrl: input.baseUrl.trim().replace(/\/+$/, ""),
      model: input.model.trim(),
      authMode: input.authMode,
      reasoningEffort: input.reasoningEffort,
      codexOptions: input.codexOptions,
      hasKey: true,
      hasBalanceToken:
        input.codexOptions?.balanceQuery?.adapter === "newapi_account" &&
        (!!input.balanceAccessToken?.trim() || !!old?.hasBalanceToken),
      keySuffix: input.apiKey?.slice(-4) || old?.keySuffix || "",
      updatedAt: Date.now() / 1000,
    };
    data.providers = [
      provider,
      ...data.providers.filter((p) => p.id !== provider.id),
    ];
    data.targets.forEach((t) => {
      if (t.activeProviderId === provider.id) {
        t.state = "saved_changes";
        t.message = "预览配置已编辑，可以重新应用。";
      }
    });
    persist(data);
    return provider;
  },
  delete: async (providerId: string): Promise<void> => {
    if (desktopRuntime) {
      await invoke("delete_provider", { providerId });
      invalidateBalance(providerId);
      return;
    }
    const data = preview();
    if (data.targets.some((t) => t.activeProviderId === providerId))
      throw new Error("此配置仍在使用，请先应用其他配置或恢复原配置");
    data.providers = data.providers.filter((p) => p.id !== providerId);
    persist(data);
  },
  prepareOverwrite: async (
    target: Target,
    providerId: string,
  ): Promise<ApplyOverwriteConfirmation> => {
    if (desktopRuntime)
      return invoke("prepare_apply_overwrite", { target, providerId });
    const status = preview().targets.find((value) => value.target === target)!;
    return {
      token: crypto.randomUUID(),
      target,
      providerId,
      directory: status.directory,
      files: status.files,
    };
  },
  apply: async (
    target: Target,
    providerId: string,
    confirmationToken?: string,
  ): Promise<TargetStatus> => {
    if (desktopRuntime)
      return invoke("apply_provider", {
        target,
        providerId,
        ...(confirmationToken ? { confirmationToken } : {}),
      });
    const data = preview();
    const supplier = data.providers.find((p) => p.id === providerId);
    if (
      supplier &&
      needsProtocolConversion(supplier, target) &&
      !protocolConversionEnabled(supplier, target)
    )
      throw new Error("请先开启当前客户端的协议转换再使用。");
    const status = data.targets.find((t) => t.target === target)!;
    status.activeProviderId = providerId;
    status.state = "applied";
    status.canRestore = true;
    status.message = "预览：已选择此配置。浏览器不会修改客户端文件。";
    persist(data);
    return status;
  },
  restore: async (target: Target): Promise<TargetStatus> => {
    if (desktopRuntime) return invoke("restore_original", { target });
    const data = preview();
    const status = data.targets.find((t) => t.target === target)!;
    status.activeProviderId = null;
    status.state = "unmanaged";
    status.canRestore = false;
    status.message = "预览：已恢复未接管状态。";
    persist(data);
    return status;
  },
  directory: async (target: Target, directory: string): Promise<void> => {
    if (desktopRuntime) return invoke("set_directory", { target, directory });
    const data = preview();
    data.targets.find((t) => t.target === target)!.directory = directory;
    persist(data);
  },
};
