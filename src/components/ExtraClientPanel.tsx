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
import { clientDefinition } from "../content/clients";
import { Modal } from "./Modal";

const protocolNames: Record<NativeProtocol, string> = {
  messages: "Claude Messages",
  gemini: "Gemini GenerateContent",
  chat_completions: "OpenAI Chat Completions",
  responses: "OpenAI Responses",
};
function nativeProtocol(
  provider: Provider,
  client: ExtraClient,
): NativeProtocol {
  const allowed = clientDefinition(client)
    .protocols as readonly NativeProtocol[];
  if (allowed.length === 1) return allowed[0];
  const upstream =
    provider.codexOptions?.upstreamProtocol ??
    (provider.family === "codex"
      ? (provider.codexOptions?.protocol ?? "openai")
      : (provider.codexOptions?.claudeProtocol ?? "anthropic"));
  if (upstream === "anthropic" && allowed.includes("messages"))
    return "messages";
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
  return responses === true && chat !== true && allowed.includes("responses")
    ? "responses"
    : allowed.includes("chat_completions")
      ? "chat_completions"
      : allowed[0];
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
  const [importLink, setImportLink] = useState("");
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
  const definition = clientDefinition(client);
  const importing = definition.mode === "import";
  const editableFiles =
    status.data?.files.filter(
      (f) =>
        ![
          "newmax",
          "alma",
          "zed_credential",
          "hanako_provider",
          "hanako_chat",
        ].includes(f.format),
    ) ?? [];
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
      if (result.importLink) setImportLink(result.importLink);
      await status.refetch();
      if (result.importLink)
        setNotice("请复制链接并在系统中打开，然后在客户端确认导入。");
      else if (result.changed) onWritten(client, result);
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
      setImportLink("");
      setRestore(false);
      await status.refetch();
      if (result.changed) onWritten(client, result);
      else
        setNotice(
          importing
            ? "本地连接已停用；已导入条目请在客户端内移除。"
            : "配置内容未变化，无需重启。",
        );
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
            使用已保存的供应商连接此客户端。供应商与 Codex、Claude Code 共用。
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
          <p>{clientDefinition(client).description}</p>
          {!!status.data?.profiles?.length && (
            <div className="extra-profile-choice">
              <label htmlFor="client-profile">配置档案 / 账户</label>
              <select
                id="client-profile"
                value={status.data.profile ?? ""}
                disabled={busy || status.data.canRestore}
                onChange={(e) => {
                  setBusy(true);
                  setError("");
                  void clientConfigApi
                    .profile(client, e.target.value)
                    .then(() => status.refetch())
                    .catch((e) => setError(errorMessage(e)))
                    .finally(() => setBusy(false));
                }}
              >
                <option value="">默认 / 当前使用</option>
                {status.data.profiles.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name} · {p.directory}
                  </option>
                ))}
              </select>
            </div>
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
              disabled={busy || !editableFiles.length}
              onClick={() => onEdit(client)}
            >
              <FileCode2 size={17} />
              查看 / 编辑配置文件
            </button>
            <button
              className="button secondary"
              disabled={
                busy ||
                status.data?.canRestore ||
                importing ||
                client === "alma"
              }
              onClick={() => {
                setError("");
                setDirectory(status.data?.directory ?? "");
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
              {importing ? "停用本地连接" : "恢复原配置"}
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
        {importLink && (
          <section className="extra-config-card">
            <label htmlFor="client-import-link">
              客户端导入链接（包含本地连接凭据）
            </label>
            <textarea
              id="client-import-link"
              readOnly
              value={importLink}
              rows={4}
            />
            <button
              className="button secondary"
              onClick={() =>
                void navigator.clipboard
                  .writeText(importLink)
                  .then(() => setNotice("已复制导入链接。"))
                  .catch(() => setError("复制失败，请手动选择并复制链接。"))
              }
            >
              复制导入链接
            </button>
          </section>
        )}
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
                  disabled={
                    busy || clientDefinition(client).protocols.length === 1
                  }
                  onChange={(e) =>
                    setProtocols((p) => ({
                      ...p,
                      [provider.id]: e.target.value as NativeProtocol,
                    }))
                  }
                >
                  {(
                    clientDefinition(client)
                      .protocols as readonly NativeProtocol[]
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
                  {clientDefinition(client).mode === "import"
                    ? "生成导入链接"
                    : "一键配置"}
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
          title={importing ? "停用本地连接" : "恢复原配置"}
          description={
            importing
              ? "停止此客户端使用本地连接。已经导入的供应商条目需要在客户端内移除。"
              : "将接管的配置恢复到首次一键配置前的备份。外部修改会阻止自动恢复，以保留当前配置。"
          }
          busy={busy}
          onClose={() => setRestore(false)}
        >
          {!importing && (
            <p>
              文件写入成功后会提示重启。手动保存过的内容也可能被本次恢复替换，请先保留所需修改。
            </p>
          )}
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
