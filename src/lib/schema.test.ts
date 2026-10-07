import { describe, expect, it } from "vitest";
import { providerSchema } from "./schema";
describe("API 地址校验", () => {
  const values = {
    name: "A",
    baseUrl: "https://example.test/v1",
    apiKey: "test-key",
    model: "model",
    authMode: "bearer",
    reasoningEffort: "",
  };
  it.each([
    "file:///secret",
    "https://user:password@example.test",
    "https://example.test?key=secret",
    "https://example.test#fragment",
  ])("拒绝不适合的地址 %s", (baseUrl) => {
    expect(providerSchema.safeParse({ ...values, baseUrl }).success).toBe(
      false,
    );
  });
  it("保留网关路径并允许本地服务", () => {
    expect(
      providerSchema.safeParse({
        ...values,
        baseUrl: "http://127.0.0.1:8000/anthropic",
      }).success,
    ).toBe(true);
  });
});
