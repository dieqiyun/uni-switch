import type { ModelProfile, ProviderModel } from "../types";

// The Go resolver owns catalog matching, supplementing and user metadata
// preferences. The UI only projects its result; protocol selection stays a
// draft setting until save and does not need to reimplement a metadata merge.
export function effectiveProfile(model: ProviderModel): ModelProfile {
  const result = { ...model.metadata?.profile };
  if (model.profileOverrides?.thinkingFormat != null)
    result.thinkingFormat = model.profileOverrides.thinkingFormat;
  return result;
}
export function preserveCapabilities(
  model: ProviderModel,
  previous?: ProviderModel,
): ProviderModel {
  return {
    ...model,
    capabilityOverrides: previous?.capabilityOverrides ?? {},
    profileOverrides: previous?.profileOverrides ?? {},
  };
}
