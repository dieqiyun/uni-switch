export type Family = "codex" | "claude";
export type Target = "codex" | "claude_desktop" | "claude_cli";
export interface Provider {
  id: string;
  family: Family;
  name: string;
  baseUrl: string;
  model: string;
  authMode: "bearer" | "x-api-key";
  reasoningEffort: string | null;
  codexOptions?: CodexOptions;
  hasKey: boolean;
  hasBalanceToken?: boolean;
  keySuffix: string;
  updatedAt: number;
  reusedExisting?: boolean;
}
export interface ProviderInput {
  id: string | null;
  family: Family;
  name: string;
  baseUrl: string;
  apiKey: string | null;
  balanceAccessToken?: string | null;
  model: string;
  authMode: "bearer" | "x-api-key";
  reasoningEffort: string | null;
  codexOptions?: CodexOptions;
}
export interface ModelCapabilities {
  imageInput?: boolean | null;
  parallelToolCalls?: boolean | null;
}
export interface ProviderModel {
  id: string;
  contextWindow: number | null;
  reasoningEfforts: string[];
  enabled: boolean;
  capabilities?: ModelCapabilities;
  capabilityOverrides?: ModelCapabilities;
}
export interface BalanceQuery {
  adapter?:
    | "auto"
    | "custom"
    | "credit"
    | "sub2api"
    | "newapi_account"
    | "newapi_token";
  siteUrl?: string | null;
  userId?: string | null;
  path: string;
  jsonPath: string;
  unit: string;
  divisor: number;
}
export interface CodexOptions {
  upstreamProtocol?: "openai" | "anthropic" | null;
  protocolPreference?: "openai" | "anthropic" | null;
  authPreference?: "bearer" | "x-api-key" | null;
  protocolDetectedAt?: number | null;
  conversionDisabledTargets?: Target[];
  protocol?: "openai" | "anthropic";
  claudeProtocol?: "openai" | "anthropic";
  repairReasoningLevels?: boolean;
  fastMode: boolean | null;
  contextWindow: number | null;
  autoCompactTokenLimit: number | null;
  models: ProviderModel[];
  modelsSyncedAt: number | null;
  balanceQuery: BalanceQuery | null;
}
export interface ConnectionInput {
  providerId: string | null;
  baseUrl: string;
  apiKey: string | null;
  balanceAccessToken?: string | null;
}

export interface ProtocolConversionResult {
  provider: Provider;
  restored: boolean;
}
export interface ModelSyncResult {
  models: ProviderModel[];
  syncedAt: number;
  protocol?: "openai" | "anthropic";
  authMode?: "bearer" | "x-api-key";
  baseUrl?: string;
}
export interface ReasoningRepairResult {
  provider: Provider;
  applied: boolean;
}
export interface FastModeResult {
  provider: Provider;
  applied: boolean;
}
export interface QuickModelInput {
  expected: Provider;
  target: Target;
  model: string;
  models: ProviderModel[];
  syncedAt?: number | null;
  repairReasoningLevels?: boolean;
}
export interface ModelWriteInput {
  connection: ConnectionInput;
  authMode: Provider["authMode"];
  model: string;
  models: ProviderModel[];
  syncedAt: number;
}
export interface BalanceResult {
  amount: number | null;
  unit: string;
  checkedAt: number;
  scope?: string;
  unlimited?: boolean;
  used?: number | null;
  total?: number | null;
  planName?: string | null;
  expiresAt?: string | null;
  providerType?: string | null;
  note?: string | null;
  windows?: {
    label: string;
    remaining: number;
    used: number;
    total: number;
    resetAt: string | null;
  }[];
}
export interface ApplyOverwriteConfirmation {
  token: string;
  target: Target;
  providerId: string;
  directory: string;
  files: string[];
}
export interface TargetStatus {
  target: Target;
  directory: string;
  files: string[];
  activeProviderId: string | null;
  state:
    | "applied"
    | "unmanaged"
    | "external_change"
    | "error"
    | "saved_changes";
  canRestore: boolean;
  message: string;
  appliedModel?: string | null;
  /** Read-back global config, not live-session or upstream usage. */
  configuredModel?: string | null;
  configuredReasoningEffort?: string | null;
  configurationRevision?: number;
}
export interface RuntimeStatus {
  target: Target;
  clientRunning: boolean;
  restartRequired: boolean;
  bridgeRequired: boolean;
  bridgeHealthy: boolean;
  configurationRevision?: number;
  desktopRunning?: boolean;
  desktopRestartRequired?: boolean;
  canRestartDesktop?: boolean;
  canRestartClient?: boolean;
  restartInProgress?: boolean;
  restartReason?: string | null;
}
export interface RestartResult {
  restarted: boolean;
  message: string;
  pending?: boolean;
}
export interface BackgroundSettings {
  supported: boolean;
  enabled: boolean;
}
export interface UpdateSource {
  currentVersion: string;
  repository: string | null;
}
export interface UpdateCheck {
  currentVersion: string;
  latestVersion: string;
  available: boolean;
  repository: string;
  releaseUrl: string;
  downloadUrl: string | null;
  remoteUpdateAvailable?: boolean;
  installerSize?: number | null;
  installInstructions?: string;
  notes: string;
  publishedAt: string | null;
  checkedAt: number;
}
export interface UpdateDownloadStatus {
  id: string;
  version: string;
  phase:
    | "checking"
    | "downloading"
    | "verifying"
    | "ready"
    | "installing"
    | "completed"
    | "cancelled"
    | "failed";
  downloaded: number;
  total: number;
  message: string;
}
export interface UpdateInstallResult {
  exitRequired: boolean;
  message: string;
}
export interface SyncTargetResult {
  target: Target;
  success: boolean;
  error?: { code: string; message: string };
}
export const automaticBalanceQuery: BalanceQuery = {
  adapter: "auto",
  path: "",
  jsonPath: "",
  unit: "USD",
  divisor: 1,
};
export interface Overview {
  providers: Provider[];
  targets: TargetStatus[];
  dataDirectory: string;
  repairedModelCapabilities?: boolean;
}

export const targetNames: Record<Target, string> = {
  codex: "Codex 桌面端 / CLI",
  claude_desktop: "Claude Code 桌面端",
  claude_cli: "Claude CLI",
};
