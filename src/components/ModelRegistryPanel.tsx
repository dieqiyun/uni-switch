import { useEffect, useRef, useState } from "react";
import { api, errorMessage } from "../lib/api";
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
        目录缓存日期：{status?.registry.verifiedAt ?? "读取中…"}
        。按 Magpie 固定版本的目录、实时资料和用户设置优先级合并。
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
          ? "使用 models.dev 的 api.json 和 models.json；每小时检查，缓存满 24 小时后刷新，失败保留缓存。目录未使用发布者签名。更新本身不写客户端配置，重新同步并保存可更新模型目录。"
          : "当前环境不更新目录；实时供应商资料和手动设置仍可用。"}
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
