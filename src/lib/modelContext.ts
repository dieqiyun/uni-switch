export const DEFAULT_MODEL_CONTEXT = 256_000;

// The editor uses k tokens; store and Codex files use integer tokens.
export function contextTokens(value: string): number | null {
  if (!/^\d+(\.\d{1,3})?$/.test(value)) return null;
  const tokens = Math.round(Number(value) * 1000);
  return Number.isSafeInteger(tokens) && tokens >= 1 && tokens <= 100_000_000
    ? tokens
    : null;
}
