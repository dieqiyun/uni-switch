import { describe, it, expect } from "vitest";
import {
  knownCapabilities,
  preserveCapabilities,
  effectiveProfile,
  knownProfile,
  installRegistry,
} from "./modelCapabilities";
import registry from "../content/model-capabilities.json";
import type { ModelRegistry, ProviderModel } from "../types";
const model: ProviderModel = {
  id: "gpt-4o",
  enabled: true,
  contextWindow: 256000,
  reasoningEfforts: [],
};
describe("模型能力解析", () => {
  it("官方能力资料仍如实记录纯文本与未知型号", () => {
    expect(knownCapabilities("gpt-4o").imageInput).toBe(true);
    expect(knownCapabilities("o3-mini").imageInput).toBe(false);
    expect(knownCapabilities("gpt-4o-custom").imageInput).toBeUndefined();
  });
  it("刷新清除旧能力开关，保留思考格式覆盖及上游观察值", () => {
    const old = {
      ...model,
      capabilityOverrides: { imageInput: false },
      profileOverrides: { thinkingFormat: "budget" as const },
    };
    const merged = preserveCapabilities(
      {
        ...model,
        capabilities: { imageInput: false },
        capabilityOverrides: { imageInput: false },
      },
      old,
    );
    expect(merged.capabilities?.imageInput).toBe(false);
    expect(merged.capabilityOverrides).toEqual({});
    expect(merged.profileOverrides).toEqual(old.profileOverrides);
  });
  it("5.5 档案保留始终思考和强制工具限制", () => {
    for (const id of [
      "claude-opus-5-5",
      "anthropic/claude-sonnet-5-5",
      "claude-opus-5-5 [1M]",
    ]) {
      expect(effectiveProfile({ ...model, id })).toMatchObject({
        thinkingFormat: "adaptive",
        thinkingAlwaysOn: true,
        forcedToolChoice: false,
        samplingParameters: false,
      });
    }
    expect(knownProfile("claude-sonnet-5-5").thinkingBetweenTools).toBe(true);
  });
  it("档案逐字段合并，保留明确 false、空档位和旧格式的上游档位", () => {
    const haiku = { ...model, id: "claude-haiku-5-5" };
    expect(effectiveProfile(haiku)).toMatchObject({
      thinkingFormat: "adaptive",
      defaultEffort: "medium",
    });
    expect(
      effectiveProfile({ ...haiku, reasoningEfforts: ["high"] })
        .reasoningEfforts,
    ).toEqual(["high"]);
    const observed = {
      ...haiku,
      profile: {
        reasoningEfforts: [],
        toolCalls: false,
        endpoints: { responses: false, messages: true },
      },
    };
    expect(effectiveProfile(observed)).toMatchObject({
      reasoningEfforts: [],
      toolCalls: false,
      endpoints: { responses: false, messages: true },
    });
    expect(effectiveProfile(observed).defaultEffort).toBeUndefined();
    expect(
      effectiveProfile({
        ...observed,
        profileOverrides: { toolCalls: true, thinkingFormat: "budget" },
      }),
    ).toMatchObject({ toolCalls: true, thinkingFormat: "budget" });
    expect(knownProfile("private/claude-haiku-5-5")).toEqual({});
    expect(
      effectiveProfile({ ...model, id: "private-model" }).thinkingFormat,
    ).toBeUndefined();
    expect(
      effectiveProfile({
        ...model,
        id: "private-model",
        officialProfile: { thinkingFormat: "budget" },
      }).thinkingFormat,
    ).toBe("budget");
  });
  it("同步只保留用户旧的思考模式覆盖，不接受上游伪造的手动值", () => {
    const previous: ProviderModel = {
      ...model,
      profileOverrides: { thinkingFormat: "budget" },
    };
    const incoming: ProviderModel = {
      ...model,
      profile: { thinkingFormat: "adaptive" },
      profileOverrides: { thinkingFormat: "none" },
    };
    expect(preserveCapabilities(incoming, previous).profileOverrides).toEqual({
      thinkingFormat: "budget",
    });
    expect(preserveCapabilities(incoming).profileOverrides).toEqual({});
    expect(
      preserveCapabilities(incoming, previous).profile?.thinkingFormat,
    ).toBe("adaptive");
  });
  it("运行时资料更新不回滚到低于最近安装的版本", () => {
    const next: ModelRegistry = {
      ...(registry as ModelRegistry),
      version: registry.version + 2,
      entries: [
        ...(registry as ModelRegistry).entries,
        {
          ids: ["new-qa-model"],
          capabilities: { imageInput: true },
          profile: { thinkingFormat: "adaptive" },
          source: "https://example.test/docs",
        },
      ],
    };
    installRegistry(next);
    expect(knownProfile("new-qa-model").thinkingFormat).toBe("adaptive");
    installRegistry({
      ...next,
      version: next.version - 1,
      entries: (registry as ModelRegistry).entries,
    });
    expect(knownCapabilities("new-qa-model").imageInput).toBe(true);
    installRegistry({
      ...next,
      schemaVersion: 2,
      version: next.version + 1,
      entries: [],
    });
    expect(knownCapabilities("new-qa-model").imageInput).toBe(true);
  });
});

describe("inline system protocol metadata", () => {
  it("keeps explicit false and manual overrides ahead of the bundled profile", () => {
    const model = {
      id: "claude-sonnet-5-5",
      reasoningEfforts: [],
    } as unknown as ProviderModel;
    expect(effectiveProfile(model).midConversationSystem).toBe(true);
    expect(
      effectiveProfile({ ...model, profile: { midConversationSystem: false } })
        .midConversationSystem,
    ).toBe(false);
    expect(
      effectiveProfile({
        ...model,
        profile: { midConversationSystem: false },
        profileOverrides: { midConversationSystem: true },
      }).midConversationSystem,
    ).toBe(true);
    expect(
      effectiveProfile({ ...model, id: "private/claude-sonnet-5-5" })
        .midConversationSystem,
    ).toBeUndefined();
  });
});
