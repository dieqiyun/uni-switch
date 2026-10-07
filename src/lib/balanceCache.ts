import type { BalanceResult } from "../types";

export const balanceCache = new Map<
  string,
  {
    providerId: string;
    until: number;
    pending: boolean;
    promise: Promise<BalanceResult>;
  }
>();
export const balanceFailures = new Map<string, number>();
const revisions = new Map<string, number>();
export const balanceRevision = (id: string | null) =>
  (id && revisions.get(id)) || 0;

export function invalidateBalance(id: string) {
  revisions.set(id, (revisions.get(id) || 0) + 1);
  for (const [key, entry] of balanceCache)
    if (entry.providerId === id) balanceCache.delete(key);
}
