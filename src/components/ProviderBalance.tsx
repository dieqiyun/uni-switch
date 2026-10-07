import { useId, useState } from "react";
import { RefreshCw } from "lucide-react";
import type { Provider } from "../types";
import { useBalance } from "../lib/useBalance";
import { BalanceValue } from "./BalanceValue";
import { formatBalanceAmount } from "../lib/formatBalance";

export function ProviderBalance({ provider }: { provider: Provider }) {
  const [expanded, setExpanded] = useState(false);
  const detailId = useId();
  const balance = useBalance(
    { providerId: provider.id, baseUrl: provider.baseUrl, apiKey: null },
    provider.codexOptions?.balanceQuery || null,
    provider.hasKey,
    provider.updatedAt,
  );
  return (
    <div className="provider-setting provider-balance" aria-busy={balance.busy}>
      <span className="provider-setting-label">
        {balance.result?.scope || "余额 / 额度"}
      </span>
      <span
        className="balance-summary"
        role="status"
        title={
          balance.result
            ? `查询于 ${new Date(balance.result.checkedAt * 1000).toLocaleString()}`
            : undefined
        }
      >
        {balance.result ? (
          `${balance.result.unlimited ? (balance.result.scope === "密钥额度" ? "密钥不限额" : "不限额") : balance.result.amount == null ? "暂不可用" : `${formatBalanceAmount(balance.result.amount)} ${balance.result.unit}`}${balance.failure ? " · 上次结果" : ""}`
        ) : (
          <span>
            {balance.preview
              ? "桌面端自动查询余额"
              : balance.busy
                ? "正在查询余额…"
                : balance.failure
                  ? balance.failureCode === "balance_unsupported"
                    ? "暂不支持余额查询"
                    : balance.failureCode === "balance_auth"
                      ? "余额查询无权限"
                      : "余额更新失败"
                  : "等待自动查询…"}
          </span>
        )}
      </span>
      <div className="provider-setting-help provider-balance-actions">
        <button
          type="button"
          className="balance-refresh"
          aria-label={`查看 ${provider.name} 余额详情`}
          aria-expanded={expanded}
          aria-controls={detailId}
          title="余额详情"
          onClick={() => setExpanded((value) => !value)}
        >
          详情
        </button>
        {!balance.preview && (
          <button
            className="balance-refresh"
            disabled={balance.busy}
            aria-label={`刷新 ${provider.name} 余额`}
            title="刷新余额"
            onClick={() => void balance.refresh()}
          >
            <RefreshCw
              size={13}
              aria-hidden
              className={balance.busy ? "spinning" : ""}
            />
            刷新
          </button>
        )}
      </div>
      <div id={detailId} className="balance-detail-content" hidden={!expanded}>
        {balance.result && (
          <BalanceValue result={balance.result} stale={!!balance.failure} />
        )}
        {balance.failure && (
          <p role="status">{balance.failure}。余额查询不影响配置接入。</p>
        )}
        {!balance.result && !balance.failure && (
          <p>余额每分钟自动刷新，也可手动刷新。</p>
        )}
      </div>
    </div>
  );
}
