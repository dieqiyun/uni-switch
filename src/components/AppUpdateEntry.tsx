import { ArrowUpRight, RefreshCw } from "lucide-react";
import type { AppUpdateState } from "../lib/useAppUpdate";
import { APP_VERSION } from "../lib/appVersion";

export function AppUpdateEntry({
  state,
  expanded,
  onClick,
}: {
  state: AppUpdateState;
  expanded: boolean;
  onClick: React.MouseEventHandler<HTMLButtonElement>;
}) {
  const { check } = state;
  const release = check.data;
  const available = !!release?.available;
  return (
    <button
      type="button"
      className={`app-version app-update-entry${available ? " has-update" : ""}`}
      aria-label={
        available
          ? `发现新版本 v${release.latestVersion}，当前版本 v${APP_VERSION}，立即更新`
          : `检查更新，当前版本 v${APP_VERSION}`
      }
      aria-haspopup="dialog"
      aria-expanded={expanded}
      title={
        available
          ? `发现新版本 v${release.latestVersion}，点击查看更新并下载`
          : "点击检查软件更新"
      }
      onClick={onClick}
    >
      <span className="app-version-label">v{APP_VERSION}</span>
      {available ? (
        <>
          <span
            className="app-update-notification"
            role="status"
            aria-live="polite"
          >
            <span className="app-update-dot" aria-hidden />
            <span>
              发现新版本 <strong>v{release.latestVersion}</strong>
            </span>
          </span>
          <span className="app-update-action">
            立即更新
            <ArrowUpRight size={14} aria-hidden />
          </span>
        </>
      ) : (
        <span className="app-update-action">
          <RefreshCw
            size={12}
            className={check.isFetching ? "spinning" : ""}
            aria-hidden
          />
          {check.isFetching ? "检查中…" : "检查更新"}
        </span>
      )}
    </button>
  );
}
