import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { FileCode2, RefreshCw, SlidersHorizontal } from "lucide-react";
import {
  clientNames,
  type ClientKind,
  type ConfigWriteResult,
  type ExtraClient,
  type NativeProtocol,
  type Provider,
} from "../types";
import { clientConfigApi } from "../lib/clientConfigApi";
import { errorMessage } from "../lib/api";
import { Modal } from "./Modal";

const protocolNames: Record<NativeProtocol, string> = {
  messages: "Claude Messages",
  chat_completions: "OpenAI Chat Completions",
  responses: "OpenAI Responses",
};
function nativeProtocol(
  provider: Provider,
  client: ExtraClient,
): NativeProtocol {
  if (client === "workbuddy") return "chat_completions";
  const upstream =
    provider.codexOptions?.upstreamProtocol ??
    (provider.family === "codex"
      ? (provider.codexOptions?.protocol ?? "openai")
      : (provider.codexOptions?.claudeProtocol ?? "anthropic"));
  if (upstream === "anthropic") return "messages";
  const selected = provider.codexOptions?.models.find(
    (m) => m.id === provider.model,
  );
  const responses =
    selected?.profileOverrides?.endpoints?.responses ??
    selected?.profile?.endpoints?.responses ??
    selected?.officialProfile?.endpoints?.responses;
  const chat =
    selected?.profileOverrides?.endpoints?.chatCompletions ??
    selected?.profile?.endpoints?.chatCompletions ??
    selected?.officialProfile?.endpoints?.chatCompletions;
  return responses === true && chat !== true ? "responses" : "chat_completions";
}
export function ExtraClientPanel({
  client,
  providers,
  onEdit,
  onWritten,
  onManage,
  onBusy,
}: {
  client: ExtraClient;
  providers: Provider[];
  onEdit: (client: ClientKind) => void;
  onWritten: (client: ClientKind, result: ConfigWriteResult) => void;
  onManage: () => void;
  onBusy: (busy: boolean) => void;
}) {
  const status = useQuery({
    queryKey: ["client-config", client],
    queryFn: () => clientConfigApi.status(client),
  });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [directory, setDirectory] = useState<string | null>(null);
  const [restore, setRestore] = useState(false);
  const [confirmation, setConfirmation] = useState<{
    provider: Provider;
    protocol: NativeProtocol;
    revision: string;
    files: string[];
  } | null>(null);
  const [protocols, setProtocols] = useState<Record<string, NativeProtocol>>(
    {},
  );
  useEffect(() => {
    onBusy(busy || directory !== null || restore || !!confirmation);
    return () => onBusy(false);
  }, [busy, directory, restore, confirmation, onBusy]);
  async function apply(
    provider: Provider,
    protocol: NativeProtocol,
    expectedRevision?: string,
  ) {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const result = await clientConfigApi.apply(
        client,
        provider.id,
        protocol,
        expectedRevision,
      );
      setConfirmation(null);
      await status.refetch();
      if (result.changed) onWritten(client, result);
      else setNotice("配置内容已是最新，无需写入或重启。");
    } catch (e) {
      if ((e as { code?: string }).code === "config_conflict") {
        const latest = await status.refetch();
        if (latest.data)
          setConfirmation({
            provider,
            protocol,
            revision: latest.data.revision,
            files: latest.data.files.map((f) => f.path),
          });
        else setError(errorMessage(e));
      } else setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  async function restoreFiles() {
    setBusy(true);
    setError("");
    try {
      const result = await clientConfigApi.restore(client);
      setRestore(false);
      await status.refetch();
      if (result.changed) onWritten(client, result);
      else setNotice("文件内容未变化，无需重启。");
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <header className="workspace-header">
        <div className="workspace-title">
          <h1>{clientNames[client]}</h1>
          <span className="supplier-total">
            供应商 <span className="count">{providers.length}</span>
          </span>
        </div>
        <div className="header-tools">
          <button
            className="button secondary"
            disabled={busy}
            onClick={onManage}
          >
            添加 / 管理供应商
          </button>
          <button
            className="icon-button"
            aria-label="刷新配置状态"
            disabled={busy || status.isFetching}
            onClick={() => void status.refetch()}
          >
            <RefreshCw size={18} />
          </button>
        </div>
      </header>
      <main className="extra-client-main">
        <section className="extra-config-card" aria-label="客户端配置">
          <h2>一键配置 {clientNames[client]}</h2>
          <p>
            使用已保存的供应商，写入客户端实际使用的配置文件。供应商与
            Codex、Claude Code 共用。
          </p>
          {client === "zcode" && (
            <p>
              个人供应商配置：provider_config.json。支持 Messages、Chat
              Completions 和 Responses，并设置默认模型。
            </p>
          )}
          {client === "dsh" && (
            <p>
              DSH（DeepSeek Harness）：在 cordis.patch.yml
              添加独立供应商路由，在 .credentials.yaml 保存凭据，并为新 agent
              设置默认模型。
            </p>
          )}
          {client === "workbuddy" && (
            <p>
              自定义模型使用 Chat Completions 接口与 Bearer 认证。保存后需在
              WorkBuddy 的模型选择器中选用。请确认下面目录与 WorkBuddy
              实际使用的位置一致；旧版使用 .codebuddy/models.json
              时可修改目录。项目级配置可能覆盖用户级配置。
            </p>
          )}
          {status.isPending && <p role="status">正在读取配置位置…</p>}
          {status.error && (
            <p className="error-notice" role="alert">
              {errorMessage(status.error)}
            </p>
          )}
          {status.data && (
            <>
              <code className="settings-path">{status.data.directory}</code>
              <p role="status">{status.data.message}</p>
            </>
          )}
          <div className="extra-config-actions">
            <button
              className="button secondary"
              disabled={busy || !status.data}
              onClick={() => onEdit(client)}
            >
              <FileCode2 size={17} />
              查看 / 编辑配置文件
            </button>
            <button
              className="button secondary"
              disabled={busy || !status.data || status.data.canRestore}
              onClick={() => {
                setError("");
                setDirectory(status.data!.directory);
              }}
            >
              <SlidersHorizontal size={17} />
              修改配置目录
            </button>
            <button
              className="button secondary"
              disabled={busy || !status.data?.canRestore}
              onClick={() => {
                setError("");
                setRestore(true);
              }}
            >
              恢复原配置
            </button>
          </div>
          {status.data?.canRestore && (
            <p>当前目录已接管，更换前请先恢复原配置。</p>
          )}
        </section>
        {error && (
          <p className="error-notice" role="alert">
            {error}
          </p>
        )}
        {notice && <p role="status">{notice}</p>}
        <section className="extra-provider-list" aria-label="已保存供应商">
          {!providers.length && (
            <div className="extra-config-card">
              <p>
                还没有供应商。先添加一组 API 地址、密钥和模型，再回来一键配置。
              </p>
              <button className="button primary" onClick={onManage}>
                添加供应商
              </button>
            </div>
          )}
          {providers.map((provider) => (
            <article className="extra-provider-row" key={provider.id}>
              <div>
                <h3>
                  {provider.name}
                  {status.data?.activeProviderId === provider.id &&
                    status.data.state === "applied" && (
                      <span className="extra-current">已写入</span>
                    )}
                </h3>
                <p>{provider.model}</p>
                <code>{provider.baseUrl}</code>
              </div>
              <div className="extra-provider-controls">
                <label htmlFor={"native-protocol-" + provider.id}>
                  接口协议
                </label>
                <select
                  id={"native-protocol-" + provider.id}
                  value={
                    protocols[provider.id] ?? nativeProtocol(provider, client)
                  }
                  disabled={busy || client === "workbuddy"}
                  onChange={(e) =>
                    setProtocols((p) => ({
                      ...p,
                      [provider.id]: e.target.value as NativeProtocol,
                    }))
                  }
                >
                  {(client === "workbuddy"
                    ? (["chat_completions"] as const)
                    : (["messages", "chat_completions", "responses"] as const)
                  ).map((p) => (
                    <option key={p} value={p}>
                      {protocolNames[p]}
                    </option>
                  ))}
                </select>
                <button
                  className="button primary"
                  disabled={busy || !status.data}
                  onClick={() =>
                    void apply(
                      provider,
                      protocols[provider.id] ??
                        nativeProtocol(provider, client),
                    )
                  }
                >
                  一键配置
                </button>
              </div>
            </article>
          ))}
        </section>
      </main>
      {directory !== null && (
        <Modal
          title={clientNames[client] + " 配置目录"}
          description="填写客户端实际使用的配置文件所在目录，保存后从该位置读取和写入。"
          busy={busy}
          onClose={() => setDirectory(null)}
        >
          <form
            onSubmit={(e) => {
              e.preventDefault();
              setBusy(true);
              setError("");
              void clientConfigApi
                .directory(client, directory)
                .then(async () => {
                  setDirectory(null);
                  await status.refetch();
                })
                .catch((e) => setError(errorMessage(e)))
                .finally(() => setBusy(false));
            }}
          >
            <label htmlFor="extra-directory">配置目录（绝对路径）</label>
            <input
              id="extra-directory"
              value={directory}
              onChange={(e) => setDirectory(e.target.value)}
              disabled={busy}
            />
            {error && (
              <p className="error-notice" role="alert">
                {error}
              </p>
            )}
            <div className="dialog-actions">
              <button
                type="button"
                className="button secondary"
                disabled={busy}
                onClick={() => setDirectory(null)}
              >
                取消
              </button>
              <button className="button primary" disabled={busy}>
                保存目录
              </button>
            </div>
          </form>
        </Modal>
      )}
      {restore && (
        <Modal
          title="恢复原配置"
          description="将接管的配置文件恢复到首次一键配置前的备份。外部修改会阻止自动恢复，以保留当前文件。"
          busy={busy}
          onClose={() => setRestore(false)}
        >
          <p>
            文件写入成功后会提示重启。手动保存过的内容也可能被本次恢复替换，请先保留所需修改。
          </p>
          {error && (
            <p className="error-notice" role="alert">
              {error}
            </p>
          )}
          <div className="dialog-actions">
            <button
              className="button secondary"
              disabled={busy}
              onClick={() => setRestore(false)}
            >
              取消
            </button>
            <button
              className="button primary"
              disabled={busy}
              onClick={() => void restoreFiles()}
            >
              确认恢复
            </button>
          </div>
        </Modal>
      )}
      {confirmation && (
        <Modal
          title="确认覆盖配置"
          description={
            "当前文件已被修改。是否重新应用「" +
            confirmation.provider.name +
            "」的配置？"
          }
          busy={busy}
          dismissOnOutside={false}
          onClose={() => setConfirmation(null)}
        >
          <p>
            当前文件会先备份。只更新此客户端，保存后提示重启。如果确认期间文件再次变化，此次写入会停止。
          </p>
          <ul>
            {confirmation.files.map((f) => (
              <li key={f}>
                <code className="settings-path">{f}</code>
              </li>
            ))}
          </ul>
          {error && (
            <p className="error-notice" role="alert">
              {error}
            </p>
          )}
          <div className="dialog-actions">
            <button
              className="button secondary"
              disabled={busy}
              onClick={() => setConfirmation(null)}
            >
              取消
            </button>
            <button
              className="button danger"
              disabled={busy}
              onClick={() =>
                void apply(
                  confirmation.provider,
                  confirmation.protocol,
                  confirmation.revision,
                )
              }
            >
              覆盖并应用
            </button>
          </div>
        </Modal>
      )}
    </>
  );
}
