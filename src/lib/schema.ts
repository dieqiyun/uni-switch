import { z } from "zod";
import { normalizeApiAddress, normalizeApiKey } from "./connectionInput";

export const providerSchema = z.object({
  name: z
    .string()
    .trim()
    .min(1, "请填写配置名称")
    .max(80, "名称最多 80 个字符"),
  baseUrl: z
    .string()
    .trim()
    .min(1, "请填写 API 地址")
    .transform(normalizeApiAddress)
    .refine((value) => {
      try {
        const u = new URL(value);
        return (
          ["http:", "https:"].includes(u.protocol) &&
          !!u.hostname &&
          !u.username &&
          !u.password &&
          !u.search &&
          !u.hash
        );
      } catch {
        return false;
      }
    }, "请输入完整的 http 或 https 地址，不包含密码、查询参数或片段"),
  apiKey: z
    .string()
    .transform(normalizeApiKey)
    .refine(
      (value) => !/[\u0000-\u001f\u007f]/.test(value),
      "密钥不能包含换行或控制字符",
    ),
  authMode: z.enum(["auto", "bearer", "x-api-key"]).default("auto"),
  protocol: z.enum(["auto", "openai", "anthropic"]).default("auto"),
  fastMode: z.enum(["", "on", "off"]).default(""),
});
export type FormValues = z.infer<typeof providerSchema>;
export type FormInput = z.input<typeof providerSchema>;
