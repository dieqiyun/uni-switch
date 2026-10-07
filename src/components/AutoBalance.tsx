import { Wallet, RefreshCw } from "lucide-react";
import type { BalanceQuery, ConnectionInput } from "../types";
import { useBalance } from "../lib/useBalance";
import { BalanceValue } from "./BalanceValue";

export function AutoBalance({
  input,
  query,
  revision = 0,
}: {
  input: ConnectionInput;
  query: BalanceQuery | null;
  revision?: number;
}) {
  let validAddress = false;
  try {
    const u = new URL(input.baseUrl);
    validAddress =
      ["http:", "https:"].includes(u.protocol) &&
      !!u.hostname &&
      !u.username &&
      !u.password &&
      !u.search &&
      !u.hash;
  } catch {
    /* Incomplete addresses remain editable without starting requests. */
  }
  const ready =
    validAddress &&
    !!(input.apiKey?.trim() || input.providerId) &&
    !/[\u0000-\u001f\u007f]/.test(input.apiKey || "");
  const balance = useBalance(input, query, ready, revision, 650);
  return (
    <section
      className="auto-balance balance-options"
      aria-label="余额查询"
      aria-busy={balance.busy}
    >
      <div className="auto-balance-heading">
        <span>
          <Wallet size={15} aria-hidden />
          <strong>余额查询</strong>
        </span>
        <span className="auto-balance-label">自动识别</span>
        {ready && !balance.preview && (
          <button
            type="button"
            className="balance-refresh"
            disabled={balance.busy}
            aria-label="刷新余额"
            onClick={() => void balance.refresh()}
          >
            <RefreshCw
              size={14}
              aria-hidden
              className={balance.busy ? "spinning" : ""}
            />
            刷新
          </button>
        )}
      </div>
      {balance.result && (
        <BalanceValue result={balance.result} stale={!!balance.failure} />
      )}
      {balance.failure ? (
        <p className="balance-failure" role="status">
          {balance.failure}
        </p>
      ) : (
        !balance.result && (
          <p className="auto-balance-hint" role="status">
            {balance.preview
              ? "桌面端会在填写地址和密钥后自动识别并查询余额。"
              : balance.busy
                ? "正在识别供应商并查询余额…"
                : ready
                  ? "即将自动查询余额…"
                  : "填写 API 地址和密钥后，自动识别供应商并查询余额。"}
          </p>
        )
      )}
    </section>
  );
}
