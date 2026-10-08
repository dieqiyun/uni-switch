import { describe, it, expect } from "vitest";
import {
  modelCapability,
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
  it("按手动、上游、官方、未知顺序，明确 false 不被自动规则覆盖", () => {
    expect(modelCapability(model, "imageInput")).toEqual({
      value: true,
      source: "official",
    });
    expect(
      modelCapability(
        { ...model, capabilities: { imageInput: false } },
        "imageInput",
      ),
    ).toEqual({ value: false, source: "upstream" });
    expect(
      modelCapability(
        {
          ...model,
          capabilities: { imageInput: true },
          capabilityOverrides: { imageInput: false },
        },
        "imageInput",
      ),
    ).toEqual({ value: false, source: "manual" });
    expect(
      modelCapability({ ...model, id: "private/gpt-4o" }, "imageInput"),
    ).toEqual({ value: false, source: "unknown" });
    expect(knownCapabilities("openai/gpt-6.1-sol").imageInput).toBe(true);
    expect(knownCapabilities("o3-mini").imageInput).toBe(false);
    expect(knownCapabilities("gpt-4o-custom").imageInput).toBeUndefined();
  });
  it("刷新更新上游能力，保留手动关闭，不把供应商返回字段当成用户设置", () => {
    const old = { ...model, capabilityOverrides: { imageInput: false } };
    const fresh = {
      ...model,
      capabilities: { imageInput: true },
      capabilityOverrides: { imageInput: true },
    };
    const merged = preserveCapabilities(fresh, old);
    expect(merged.capabilities?.imageInput).toBe(true);
    expect(modelCapability(merged, "imageInput")).toEqual({
      value: false,
      source: "manual",
    });
    expect(preserveCapabilities(fresh).capabilityOverrides).toEqual({});
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
