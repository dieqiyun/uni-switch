import type { Provider, Target } from "../types";
export function providerProtocol(provider: Provider): "openai" | "anthropic" {
  return (
    provider.codexOptions?.upstreamProtocol ||
    (provider.family === "codex"
      ? provider.codexOptions?.protocol || "openai"
      : provider.codexOptions?.claudeProtocol || "anthropic")
  );
}

export function needsProtocolConversion(provider: Provider, target: Target) {
  return (target === "codex") !== (providerProtocol(provider) === "openai");
}

export function protocolConversionEnabled(provider: Provider, target: Target) {
  return !provider.codexOptions?.conversionDisabledTargets?.includes(target);
}

export function protocolConfirmed(provider: Provider) {
  const options = provider.codexOptions;
  return (
    !!options?.protocolPreference ||
    (!!options?.upstreamProtocol &&
      !!(options.protocolDetectedAt || options.modelsSyncedAt))
  );
}
