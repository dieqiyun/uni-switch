import { invoke } from "@tauri-apps/api/core";
import { desktopRuntime } from "./api";
import type {
  ClientKind,
  ClientConfigStatus,
  ConfigDocument,
  ConfigWriteResult,
  ExtraClient,
  NativeProtocol,
} from "../types";
const desktop = () => {
  if (!desktopRuntime)
    throw new Error("请在 uni-switch 桌面应用中读取和修改客户端配置文件。");
};
export const clientConfigApi = {
  status: async (client: ClientKind): Promise<ClientConfigStatus> => {
    if (!desktopRuntime)
      return {
        client,
        directory: "桌面应用中显示实际配置路径",
        files: [],
        activeProviderId: null,
        canRestore: false,
        state: "unmanaged",
        message: "浏览器预览不会访问本机配置文件。",
        revision: "preview",
      };
    return invoke("get_client_config_status", { client });
  },
  read: async (client: ClientKind, fileId: string): Promise<ConfigDocument> => {
    desktop();
    return invoke("read_client_config", { client, fileId });
  },
  save: async (document: ConfigDocument): Promise<ConfigWriteResult> => {
    desktop();
    return invoke("save_client_config", { document });
  },
  directory: async (client: ClientKind, directory: string): Promise<void> => {
    desktop();
    return invoke("set_client_config_directory", { client, directory });
  },
  apply: async (
    client: ExtraClient,
    providerId: string,
    protocol: NativeProtocol,
    expectedRevision?: string,
  ): Promise<ConfigWriteResult> => {
    desktop();
    return invoke("apply_client_config", {
      client,
      providerId,
      protocol,
      expectedRevision: expectedRevision ?? null,
    });
  },
  restore: async (client: ExtraClient): Promise<ConfigWriteResult> => {
    desktop();
    return invoke("restore_client_config", { client });
  },
};
