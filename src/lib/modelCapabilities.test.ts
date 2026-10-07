import { describe, it, expect } from "vitest";
import {
  modelCapability,
  knownCapabilities,
  preserveCapabilities,
} from "./modelCapabilities";
import type { ProviderModel } from "../types";
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
});
