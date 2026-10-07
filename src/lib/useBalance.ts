import { useEffect, useRef, useState } from "react";
import { api, desktopRuntime, errorMessage } from "./api";
import {
  balanceCache as cache,
  balanceRevision,
  balanceFailures,
} from "./balanceCache";
import { errorCode } from "./feedback";
import type { BalanceQuery, BalanceResult, ConnectionInput } from "../types";

let active = 0;
const queue: (() => void)[] = [];
async function limited<T>(run: () => Promise<T>): Promise<T> {
  if (active >= 3) await new Promise<void>((resolve) => queue.push(resolve));
  else active++;
  try {
    return await run();
  } finally {
    const next = queue.shift();
    if (next) next();
    else active--;
  }
}
function request(
  input: ConnectionInput,
  query: BalanceQuery | null,
  revision: number,
  force: boolean,
) {
  // Cache saved-provider results without retaining credentials in cache keys.
  const id =
    input.providerId && !input.apiKey && !input.balanceAccessToken
      ? JSON.stringify([
          input.providerId,
          input.baseUrl,
          query,
          revision,
          balanceRevision(input.providerId),
        ])
      : null;
  const previous = id && cache.get(id);
  if (previous && (previous.pending || (!force && previous.until > Date.now())))
    return previous.promise;
  const promise = limited(() => api.balance(input, query));
  if (id) {
    for (const [key, entry] of cache)
      if (entry.until < Date.now()) cache.delete(key);
    if (cache.size >= 100) cache.delete(cache.keys().next().value!);
    const entry = {
      providerId: input.providerId!,
      until: Date.now() + 30_000,
      promise,
      pending: true,
    };
    cache.set(id, entry);
    void promise.then(
      () => {
        entry.pending = false;
        entry.until = Date.now() + 30000;
        balanceFailures.delete(id);
      },
      (error) => {
        entry.pending = false;
        const failures = (balanceFailures.get(id) || 0) + 1;
        balanceFailures.set(id, failures);
        entry.until =
          Date.now() +
          (errorCode(error) === "balance_unsupported"
            ? 1800000
            : errorCode(error) === "balance_auth"
              ? 900000
              : Math.min(600000, 60000 * 2 ** Math.min(failures - 1, 4)));
      },
    );
  }
  return promise;
}
export function useBalance(
  input: ConnectionInput,
  query: BalanceQuery | null,
  ready: boolean,
  revision = 0,
  debounce = 0,
) {
  const [state, setState] = useState<{
    busy: boolean;
    result: BalanceResult | null;
    failure: string;
    failureCode: string;
  }>({ busy: false, result: null, failure: "", failureCode: "" });
  const generation = useRef(0);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const current = useRef({ input, query, ready, revision });
  current.current = { input, query, ready, revision };
  const signature = JSON.stringify([
    input,
    query,
    ready,
    revision,
    balanceRevision(input.providerId),
  ]);
  async function refresh(force = true) {
    if (timer.current !== null) {
      clearTimeout(timer.current);
      timer.current = null;
    }
    const params = current.current;
    if (!params.ready || !desktopRuntime) return;
    const requestGeneration = ++generation.current;
    setState((s) => ({ ...s, busy: true }));
    try {
      const result = await request(
        params.input,
        params.query,
        params.revision,
        force,
      );
      if (requestGeneration === generation.current)
        setState({ busy: false, result, failure: "", failureCode: "" });
    } catch (e) {
      if (requestGeneration === generation.current)
        setState((s) => ({
          ...s,
          busy: false,
          failure: errorMessage(e),
          failureCode: errorCode(e),
        }));
    }
  }
  useEffect(() => {
    generation.current++;
    setState({ busy: false, result: null, failure: "", failureCode: "" });
    if (!ready || !desktopRuntime) return;
    timer.current = setTimeout(() => void refresh(false), debounce);
    return () => {
      if (timer.current !== null) clearTimeout(timer.current);
      timer.current = null;
      generation.current++;
    };
    // Signature includes all request inputs; changes discard outdated results.
  }, [signature, debounce]);
  useEffect(() => {
    if (!desktopRuntime || !ready || !input.providerId || input.apiKey) return;
    const interval = setInterval(() => {
      if (document.visibilityState === "visible") void refresh(false);
    }, 60000);
    const focus = () => {
      if (document.visibilityState === "visible") void refresh(false);
    };
    window.addEventListener("focus", focus);
    return () => {
      clearInterval(interval);
      window.removeEventListener("focus", focus);
    };
  }, [signature]);
  return { ...state, refresh: () => refresh(true), preview: !desktopRuntime };
}
