import type { BalanceResult } from "../types";
import { formatBalanceAmount } from "../lib/formatBalance";

const number = (value: number) => formatBalanceAmount(value, 6);
function date(value: string) {
  const d = new Date(/^\d+$/.test(value) ? Number(value) * 1000 : value);
  return Number.isNaN(d.getTime()) ? value : d.toLocaleString();
}
export function BalanceValue({
  result,
  stale = false,
}: {
  result: BalanceResult;
  stale?: boolean;
}) {
  return (
    <span
      className="balance-value"
      role="status"
      title={`查询于 ${new Date(result.checkedAt * 1000).toLocaleString()}`}
    >
      <span className="balance-main">
        {result.scope || "余额"} ·{" "}
        {result.unlimited
          ? result.scope === "密钥额度"
            ? "密钥不限额"
            : "不限额"
          : result.amount == null
            ? "暂不可用"
            : `${number(result.amount)} ${result.unit}`}
        {stale ? " · 上次结果" : ""}
      </span>
      <span className="balance-checked">
        {stale ? "上次成功查询" : "查询时间"}：
        {new Date(result.checkedAt * 1000).toLocaleString()}
      </span>
      {(result.used != null ||
        result.planName ||
        result.expiresAt ||
        result.providerType) && (
        <span className="balance-meta">
          {result.providerType && <span>{result.providerType}</span>}
          {result.planName && <span>{result.planName}</span>}
          {result.used != null && (
            <span>
              已用 {number(result.used)} {result.unit}
            </span>
          )}
          {result.expiresAt && <span>到期 {date(result.expiresAt)}</span>}
        </span>
      )}
      {result.note && <span className="balance-note">{result.note}</span>}
      {!!result.windows?.length && (
        <span className="balance-windows">
          {result.windows.map((w, i) => (
            <span
              key={`${w.label}-${i}`}
              title={`已用 ${number(w.used)} / ${number(w.total)} ${result.unit}${w.resetAt ? ` · 重置于 ${date(w.resetAt)}` : ""}`}
            >
              {w.label}剩余 {number(w.remaining)} {result.unit}
            </span>
          ))}
        </span>
      )}
    </span>
  );
}
