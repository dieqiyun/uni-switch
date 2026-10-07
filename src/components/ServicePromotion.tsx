import { useState } from "react";
import { ArrowUpRight } from "lucide-react";
import { api, desktopRuntime, errorMessage } from "../lib/api";
import familyLogo from "../assets/dieqiyun-logo.png";

export const SERVICE_WEBSITE = "https://www.dieqiyun.top/";

export function ServicePromotion() {
  const [opening, setOpening] = useState(false);
  const [error, setError] = useState<string | null>(null);
  async function open(event: React.MouseEvent<HTMLAnchorElement>) {
    if (!desktopRuntime) return;
    event.preventDefault();
    if (opening) return;
    setOpening(true);
    setError(null);
    try {
      await api.openServiceWebsite();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setOpening(false);
    }
  }
  return (
    <div className="service-promotion">
      <a
        className="service-promotion-link"
        href={SERVICE_WEBSITE}
        target="_blank"
        rel="noopener noreferrer"
        aria-label="访问蝶祈云 API 官网（在浏览器中打开）"
        aria-disabled={opening || undefined}
        aria-busy={opening || undefined}
        onClick={(event) => void open(event)}
      >
        <img className="service-promotion-logo" src={familyLogo} alt="" width="34" height="34" />
        <span className="service-promotion-copy">
          <span className="service-promotion-title">
            蝶祈云 API<span>GPT · Claude · Gemini</span>
          </span>
          <span className="service-promotion-description">
            GPT 真官 Key · 机构合作定制 · 优质渠道聚合 · 支持开具发票
          </span>
        </span>
        <span className="service-promotion-action">
          {opening ? "打开中…" : "访问官网"}
          <ArrowUpRight size={14} aria-hidden />
        </span>
      </a>
      {error && (
        <p className="service-promotion-error" role="alert">
          {error} 官网：{SERVICE_WEBSITE}
        </p>
      )}
    </div>
  );
}
