import { describe, it, expect } from "vitest";
import { effectiveProfile, preserveCapabilities } from "./modelCapabilities";
import type { ProviderModel } from "../types";
const model: ProviderModel = {
  id: "fixture",
  enabled: true,
  contextWindow: null,
  reasoningEfforts: [],
};
describe("Go 资料投影", () => {
  it("前端不再用旧表和型号名称推断资料", () => {
    expect(
      effectiveProfile({
        ...model,
        id: "claude-opus-5-5",
        profile: { contextWindow: 9999 },
        officialProfile: { maxOutputTokens: 1000 },
      }),
    ).toEqual({});
  });
  it("如实展示后端合并后的未知、否和供应商档位", () => {
    const metadata = {
      policy: "magpie-model-spec-v1",
      reference: "fixture",
      vendor: {},
      profile: {
        contextWindow: 4095,
        reasoningEfforts: ["light", "extra_high"],
        toolCalls: null,
      },
      capabilities: { imageInput: false },
      sources: { contextWindow: "vendor" },
    };
    const result = effectiveProfile({ ...model, metadata });
    expect(result.contextWindow).toBe(4095);
    expect(result.reasoningEfforts).toEqual(["light", "extra_high"]);
    expect(result.toolCalls).toBeNull();
  });
  it("只即时投影草稿中的协议格式，不另做模型资料优先级合并", () => {
    const m = {
      ...model,
      metadata: {
        policy: "magpie-model-spec-v1",
        reference: "fixture",
        vendor: {},
        profile: { thinkingFormat: "adaptive" as const, contextWindow: 8192 },
        capabilities: {},
        sources: {},
      },
      profileOverrides: {
        thinkingFormat: "budget" as const,
        contextWindow: 99999,
      },
    };
    expect(effectiveProfile(m)).toMatchObject({
      thinkingFormat: "budget",
      contextWindow: 8192,
    });
  });
  it("刷新保留用户明确设置，包括 false 和自动重置", () => {
    const previous = {
      ...model,
      capabilityOverrides: { imageInput: false },
      profileOverrides: { thinkingFormat: null, maxOutputTokens: 8192 },
    };
    const incoming = { ...model, profile: { contextWindow: 4095 } };
    expect(preserveCapabilities(incoming, previous)).toMatchObject({
      capabilityOverrides: { imageInput: false },
      profileOverrides: { thinkingFormat: null, maxOutputTokens: 8192 },
      profile: { contextWindow: 4095 },
    });
    expect(preserveCapabilities(incoming).profileOverrides).toEqual({});
  });
});
