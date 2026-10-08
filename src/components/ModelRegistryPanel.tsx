import { useEffect, useRef, useState } from "react";
import { api, errorMessage } from "../lib/api";
import { installRegistry } from "../lib/modelCapabilities";
import type { ModelRegistryStatus } from "../types";

export function ModelRegistryPanel() {
  const [status, setStatus] = useState<ModelRegistryStatus | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const active = useRef(false);
  useEffect(() => {
    active.current = true;
    void api
      .modelRegistry()
      .then((next) => {
        if (active.current) {
          installRegistry(next.registry);
          setStatus(next);
        }
      })
      .catch((reason) => {
        if (active.current) setError(errorMessage(reason));
      });
    return () => {
      active.current = false;
    };
  }, []);
  async function update() {
    setBusy(true);
    setError("");
    try {
      const next = await api.updateModelRegistry();
      if (active.current) {
        installRegistry(next.registry);
        setStatus(next);
      }
    } catch (reason) {
      if (active.current) setError(errorMessage(reason));
    } finally {
      if (active.current) setBusy(false);
    }
  }
  return (
    <section
      className="settings-section"
      aria-labelledby="model-registry-heading"
      aria-busy={busy}
    >
      <h3 id="model-registry-heading">模型能力资料</h3>
      <p>
        核实日期：{status?.registry.verifiedAt ?? "读取中…"}
        。上游明确元数据优先，手动设置不会被刷新覆盖。
      </p>
      <button
        type="button"
        className="button secondary"
        disabled={busy || !status?.updateAvailable}
        aria-describedby="model-registry-hint"
        onClick={() => void update()}
      >
        {busy ? "更新中…" : "更新模型资料"}
      </button>
      <p id="model-registry-hint">
        {status?.updateAvailable
          ? "每天检查签名资料更新；失败保留离线资料。更新本身不写客户端配置，重新同步并保存可更新模型目录。"
          : "发布者尚未配置签名更新源。当前使用内置资料，上游模型同步和手动设置仍可用。"}
      </p>
      {status?.checkedAt && (
        <p>最近检查：{new Date(status.checkedAt * 1000).toLocaleString()}</p>
      )}
      {error ? (
        <p role="alert">{error}</p>
      ) : (
        <p role="status">{status?.message}</p>
      )}
    </section>
  );
}
