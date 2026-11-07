import { invoke } from "@tauri-apps/api/core";
import { desktopRuntime } from "./api";
export interface SubscriptionAccount {
  id: string;
  kind: string;
  name: string;
  user: string;
  enabled: boolean;
  project?: string;
  expiry?: number;
  active: boolean;
}
export interface Login {
  id: string;
  kind: string;
  state: string;
  url?: string;
  code?: string;
  message?: string;
}
export interface GatewaySource {
  builtin?: boolean;
  id: string;
  name: string;
  baseUrl: string;
  protocol: "messages" | "responses" | "chat" | "gemini";
  authMode: string;
  apiKey?: string;
  account?: string;
  vendor: string;
  models: { id: string; kind: string }[];
  enabled: boolean;
}
export async function platformCall<T>(
  action: string,
  params: Record<string, unknown> = {},
): Promise<T> {
  if (!desktopRuntime) {
    if (action === "accounts" || action === "sources") return [] as T;
    throw new Error("请在桌面应用中管理订阅账号与统一网关。");
  }
  return invoke<T>("platform_command", { action, params });
}
