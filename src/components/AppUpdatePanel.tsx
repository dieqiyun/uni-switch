import { useState } from "react";
import { ExternalLink, RefreshCw } from "lucide-react";
import type { AppUpdateState } from "../lib/useAppUpdate";
import { APP_VERSION } from "../lib/appVersion";
import { api, desktopRuntime, errorMessage } from "../lib/api";

export function AppUpdatePanel({
  state,
  showHeading = true,
}: {
  state: AppUpdateState;
  showHeading?: boolean;
}) {
  const { source, check } = state;
  const [opening, setOpening] = useState(false);
  const [openError, setOpenError] = useState("");
  const release = check.data;
  const NotesHeading = showHeading ? "h4" : "h3";
  const currentVersion = source.data?.currentVersion || APP_VERSION;
  const configured = !!source.data?.repository;
  const sourceError = source.error;
  const error = sourceError || check.error;
  const releaseUrl =
    release?.releaseUrl ||
    (configured
      ? `https://github.com/${source.data!.repository}/releases`
      : "");
  async function open() {
    if (opening || !releaseUrl) return;
    setOpening(true);
    setOpenError("");
    try {
      await api.openRelease(releaseUrl);
    } catch (error) {
      setOpenError(errorMessage(error));
    } finally {
      setOpening(false);
    }
  }
  return (
    <section
      className="settings-section app-update-panel"
      aria-labelledby={showHeading ? "app-update-heading" : undefined}
      aria-label={showHeading ? undefined : "版本信息"}
    >
      {showHeading && <h3 id="app-update-heading">软件更新</h3>}
      <div className="app-update-status" role="status" aria-live="polite">
        <span>
          当前版本 <strong>v{currentVersion}</strong>
        </span>
        {release?.available && (
          <span className="update-version">
            新版本 v{release.latestVersion}
          </span>
        )}
        {!check.isFetching && !error && release && !release.available && (
          <span>已是最新版本</span>
        )}
        {check.isFetching && <span>正在检查 GitHub…</span>}
      </div>
      <p id="app-update-hint">
        {!desktopRuntime
          ? "请在桌面应用中检测更新。"
          : source.isPending
            ? "正在读取更新来源…"
            : !configured && !sourceError
              ? "此构建尚未绑定 GitHub 发布仓库，绑定后即可检测更新。"
              : "启动时自动检查 GitHub 正式版本；更新后按发布页说明安装。"}
      </p>
      <div className="app-update-actions">
        <button
          className="button secondary"
          disabled={
            check.isFetching ||
            source.isFetching ||
            !desktopRuntime ||
            (!configured && !sourceError)
          }
          aria-describedby="app-update-hint"
          onClick={() =>
            void (sourceError ? source.refetch() : check.refetch())
          }
        >
          <RefreshCw
            size={15}
            className={check.isFetching ? "spinning" : ""}
            aria-hidden
          />
          {check.isFetching ? "检测中…" : sourceError ? "重试" : "检测更新"}
        </button>
        {configured && (
          <button
            className={`button ${release?.available ? "primary" : "quiet"}`}
            disabled={opening}
            onClick={() => void open()}
          >
            <ExternalLink size={15} aria-hidden />
            {opening
              ? "打开中…"
              : release?.available
                ? "前往下载"
                : "查看发布页"}
          </button>
        )}
      </div>
      {!!error && (
        <p className="app-update-error" role="alert">
          {errorMessage(error)}
        </p>
      )}
      {openError && (
        <div className="app-update-open-error">
          <p className="app-update-error" role="alert">
            {openError}
          </p>
          <label htmlFor="app-release-url">发布页地址</label>
          <input
            id="app-release-url"
            value={releaseUrl}
            readOnly
            onFocus={(event) => event.currentTarget.select()}
          />
        </div>
      )}
      {release && (
        <p className="app-update-checked">
          上次检查：
          {new Date(release.checkedAt * 1000).toLocaleString("zh-CN", {
            hour12: false,
          })}
        </p>
      )}
      {release?.available && release.notes && (
        <div className="app-update-notes">
          <NotesHeading>更新说明</NotesHeading>
          <p>{release.notes}</p>
        </div>
      )}
    </section>
  );
}
