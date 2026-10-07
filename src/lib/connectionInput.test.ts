import { describe, expect, it } from "vitest";
import { normalizeApiAddress, normalizeApiKey } from "./connectionInput";
import { providerSchema } from "./schema";
import { formatBalanceAmount } from "./formatBalance";
describe("接入信息整理", () => {
  it("极小非零额度和确切零有不同显示", () => {
    expect(formatBalanceAmount(0)).toBe("0");
    expect(formatBalanceAmount(0.00001)).toBe("<0.01");
    expect(formatBalanceAmount(0.000000001, 6)).toBe("<0.000001");
    expect(formatBalanceAmount(-0.001)).toBe("−<0.01");
  });
  it.each(["models", "responses", "messages", "chat/completions"])(
    "完整%s接口保留网关前缀、端口和v1",
    (endpoint) => {
      expect(
        normalizeApiAddress(
          ` https://gateway.test:8443/team/openai/v1/${endpoint}/ `,
        ),
      ).toBe("https://gateway.test:8443/team/openai/v1");
    },
  );
  it("不改动普通路径，不移除查询参数或URL中的认证信息", () => {
    for (const url of [
      "https://gateway.test/model-service",
      "https://gateway.test/v1/models?key=private",
      "https://user:pass@gateway.test/v1/models",
      "https://gateway.test/v1/models#private",
    ])
      expect(normalizeApiAddress(url)).toBe(url);
  });
  it("Enter提交也使用整理后的地址与密钥，内部换行仍被拒绝", () => {
    const values = providerSchema.parse({
      name: "test",
      baseUrl: "https://gateway.test/v1/responses",
      apiKey: ' "Bearer fake-token" \n',
    });
    expect(values.baseUrl).toBe("https://gateway.test/v1");
    expect(values.apiKey).toBe("fake-token");
    expect(normalizeApiKey("fakeBearer-token")).toBe("fakeBearer-token");
    expect(
      providerSchema.safeParse({ ...values, apiKey: "fake\nkey" }).success,
    ).toBe(false);
  });
});
