import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { clientConfigApi } from "../lib/clientConfigApi";
import { errorMessage } from "../lib/api";
import {
  clientNames,
  type ClientKind,
  type ConfigDocument,
  type ConfigWriteResult,
} from "../types";
import { Modal } from "./Modal";

export function ClientConfigEditor({
  client,
  onClose,
  onSaved,
}: {
  client: ClientKind;
  onClose: () => void;
  onSaved: (result: ConfigWriteResult) => void;
}) {
  const status = useQuery({
    queryKey: ["client-config", client],
    queryFn: () => clientConfigApi.status(client),
  });
  const [document, setDocument] = useState<ConfigDocument | null>(null);
  const [content, setContent] = useState("");
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [discard, setDiscard] = useState<{ fileId?: string } | null>(null);
  const dirty = !!document && content !== document.content;
  async function load(fileId: string) {
    setBusy(true);
    setError("");
    setMessage("");
    try {
      const value = await clientConfigApi.read(client, fileId);
      setDocument(value);
      setContent(value.content);
      setEditing(false);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  function select(fileId: string) {
    if (dirty) setDiscard({ fileId });
    else void load(fileId);
  }
  function close() {
    if (dirty) setDiscard({});
    else onClose();
  }
  async function save() {
    if (!document) return;
    setBusy(true);
    setError("");
    try {
      const result = await clientConfigApi.save({ ...document, content });
      if (result.changed) {
        onSaved(result);
      } else {
        setMessage("内容未变化，无需保存或重启。");
        setEditing(false);
      }
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal
      title={clientNames[client] + " 配置文件"}
      description="查看实际文件路径和内容。保存前校验格式并备份原文件，保存后提示重启客户端。"
      wide
      className="config-editor-modal"
      busy={busy}
      dismissOnOutside={false}
      onClose={close}
    >
      <p className="config-privacy">
        文件内容可能包含 API Key。仅在本机读取，请勿分享包含密钥的截图或内容。
      </p>
      {status.isPending && <p role="status">正在读取配置文件列表…</p>}
      {status.error && (
        <p className="error-notice" role="alert">
          {errorMessage(status.error)}
        </p>
      )}
      <div className="config-file-list" role="group" aria-label="配置文件列表">
        {status.data?.files.map((file) => (
          <button
            key={file.id}
            className="config-file-choice"
            disabled={busy || !!discard}
            aria-pressed={document?.fileId === file.id}
            onClick={() => select(file.id)}
          >
            <code>{file.path}</code>
            <small>
              {file.format.toUpperCase()} ·{" "}
              {file.exists ? "已存在" : "尚未创建，保存时创建"}
            </small>
          </button>
        ))}
      </div>
      {status.data && !status.data.files.length && (
        <p>请在桌面应用中查看本机配置文件。</p>
      )}
      {!document && !!status.data?.files.length && (
        <p>选择一个文件后显示内容。</p>
      )}
      {document && (
        <div className="config-editor-body">
          <label htmlFor="client-config-content">
            {document.format.toUpperCase()} 文件内容
            {dirty ? " · 有未保存修改" : editing ? " · 编辑模式" : " · 只读"}
          </label>
          <textarea
            id="client-config-content"
            className="config-source"
            spellCheck={false}
            autoCapitalize="off"
            autoCorrect="off"
            readOnly={!editing || busy}
            value={content}
            onChange={(e) => setContent(e.target.value)}
          />
          {!document.exists && (
            <p>文件尚未创建。只查看不会创建文件，点击保存后才写入。</p>
          )}
        </div>
      )}
      {error && (
        <p className="error-notice" role="alert">
          {error}
        </p>
      )}
      {message && <p role="status">{message}</p>}
      {discard ? (
        <div className="config-discard" role="alert">
          <p>还有未保存的修改，是否放弃？</p>
          <div className="dialog-actions">
            <button
              className="button secondary"
              onClick={() => setDiscard(null)}
            >
              继续编辑
            </button>
            <button
              className="button danger"
              onClick={() => {
                const pending = discard;
                setDiscard(null);
                if (pending.fileId) void load(pending.fileId);
                else onClose();
              }}
            >
              放弃未保存修改
            </button>
          </div>
        </div>
      ) : (
        <div className="dialog-actions">
          <button className="button secondary" disabled={busy} onClick={close}>
            关闭
          </button>
          {document && (
            <>
              {editing ? (
                <>
                  <button
                    className="button secondary"
                    disabled={busy}
                    onClick={() => select(document.fileId)}
                  >
                    重新读取
                  </button>
                  <button
                    className="button primary"
                    disabled={busy || (!dirty && document.exists)}
                    onClick={() => void save()}
                  >
                    {busy ? "正在保存…" : "保存配置"}
                  </button>
                </>
              ) : (
                <button
                  className="button primary"
                  disabled={busy}
                  onClick={() => setEditing(true)}
                >
                  开始编辑
                </button>
              )}
            </>
          )}
        </div>
      )}
    </Modal>
  );
}

export function ConfigRestartNotice({
  client,
  result,
  onClose,
}: {
  client: ClientKind;
  result: ConfigWriteResult;
  onClose: () => void;
}) {
  return (
    <Modal
      title={clientNames[client] + " 配置已保存"}
      description="请完全退出并重新打开客户端，再新开对话或任务，以加载新配置。"
      onClose={onClose}
    >
      <p>
        正在进行的对话可能继续使用旧模型、地址或思考设置。关闭此提示不会自动停止任何进程。
      </p>
      {client === "workbuddy" && (
        <p>重新打开后，请在 WorkBuddy 的模型选择器中选择刚添加的自定义模型。</p>
      )}
      {result.backupPath && (
        <>
          <p>原文件备份</p>
          <code className="settings-path">{result.backupPath}</code>
        </>
      )}
      <div className="dialog-actions">
        <button className="button primary" onClick={onClose}>
          知道了
        </button>
      </div>
    </Modal>
  );
}
