import { useEffect, useRef, useState } from "react";
import { Download, ExternalLink, RefreshCw } from "lucide-react";
import type { UpdateDownloadStatus } from "../types";
import type { AppUpdateState } from "../lib/useAppUpdate";
import { APP_VERSION } from "../lib/appVersion";
import { api, desktopRuntime, errorMessage } from "../lib/api";

export function AppUpdatePanel({
  state,
  showHeading = true,
  onBusy,
}: {
  state: AppUpdateState;
  showHeading?: boolean;
  onBusy?: (busy: boolean) => void;
}) {
  const { source, check } = state;
  const [opening, setOpening] = useState(false);
  const [openError, setOpenError] = useState("");
  const [download, setDownload] = useState<UpdateDownloadStatus | null>(null);
  const [downloadError, setDownloadError] = useState("");
  const [starting, setStarting] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const [installing, setInstalling] = useState(false);
  const actionLock = useRef(false);
  const active =
    !!download &&
    ["checking", "downloading", "verifying"].includes(download.phase);
  const busy = active || starting || installing || cancelling;
  useEffect(() => {
    onBusy?.(busy);
  }, [busy, onBusy]);
  useEffect(() => {
    let mounted = true;
    if (desktopRuntime)
      void api
        .updateDownloadStatus()
        .then((value) => {
          if (
            mounted &&
            value &&
            !["cancelled", "failed", "completed"].includes(value.phase)
          )
            setDownload(value);
        })
        .catch((error) => {
          if (mounted) setDownloadError(errorMessage(error));
        });
    return () => {
      mounted = false;
    };
  }, []);
  useEffect(() => {
    if (!active) return;
    let mounted = true;
    let fetching = false;
    const timer = setInterval(() => {
      if (fetching) return;
      fetching = true;
      void api
        .updateDownloadStatus()
        .then((value) => {
          if (mounted && value?.id === download?.id) {
            setDownload(value);
            setDownloadError("");
          }
        })
        .catch((error) => {
          if (mounted) setDownloadError(errorMessage(error));
        })
        .finally(() => {
          fetching = false;
        });
    }, 400);
    return () => {
      mounted = false;
      clearInterval(timer);
    };
  }, [active, download?.id]);
  const release = check.data;
  const choices =
    !busy && (!download || ["cancelled", "failed"].includes(download.phase));
  async function startDownload() {
    if (actionLock.current || !release?.remoteUpdateAvailable || !choices)
      return;
    actionLock.current = true;
    setStarting(true);
    setDownloadError("");
    setOpenError("");
    try {
      setDownload(await api.startUpdateDownload(release.latestVersion));
    } catch (error) {
      setDownloadError(errorMessage(error));
    } finally {
      setStarting(false);
      actionLock.current = false;
    }
  }
  async function cancelDownload() {
    if (actionLock.current || !download) return;
    actionLock.current = true;
    setCancelling(true);
    try {
      await api.cancelUpdateDownload(download.id);
      setDownload(null);
      setDownloadError("");
    } catch (error) {
      setDownloadError(errorMessage(error));
    } finally {
      setCancelling(false);
      actionLock.current = false;
    }
  }
  async function installDownload() {
    if (actionLock.current || download?.phase !== "ready") return;
    actionLock.current = true;
    setInstalling(true);
    setDownloadError("");
    try {
      const result = await api.installUpdate(download.id);
      setDownload({ ...download, phase: "completed", message: result.message });
    } catch (error) {
      setDownloadError(errorMessage(error));
      const value = await api.updateDownloadStatus().catch(() => null);
      if (value?.id === download.id) setDownload(value);
    } finally {
      setInstalling(false);
      actionLock.current = false;
    }
  }
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
    if (opening || busy || !releaseUrl) return;
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
              : "启动时只检查版本。每次更新由你选择 GitHub 手动下载或远程更新，不会自动下载或安装。"}
      </p>
      <div className="app-update-actions">
        <button
          className="button secondary"
          disabled={
            busy ||
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
        {configured && (!release?.available || choices) && (
          <button
            className={`button ${release?.available ? "primary" : "quiet"}`}
            disabled={opening || busy}
            onClick={() => void open()}
          >
            <ExternalLink size={15} aria-hidden />
            {opening
              ? "打开中…"
              : release?.available
                ? "GitHub 手动下载"
                : "查看发布页"}
          </button>
        )}
        {release?.available && choices && (
          <button
            className="button secondary"
            disabled={
              !release.remoteUpdateAvailable || !desktopRuntime || opening
            }
            aria-describedby="remote-update-hint"
            onClick={() => void startDownload()}
          >
            <Download size={15} aria-hidden />
            远程更新
          </button>
        )}
      </div>
      {release?.available && choices && (
        <p id="remote-update-hint" className="scope-hint">
          {release.remoteUpdateAvailable
            ? "远程更新由软件下载并校验本系统安装包，完成后由你确认安装。"
            : "此版本未提供可校验的本系统安装包，请选择 GitHub 手动下载。"}
        </p>
      )}
      {starting && <p role="status">正在准备更新…</p>}
      {download && !["cancelled", "failed"].includes(download.phase) && (
        <div className="remote-update-progress">
          <h3>远程更新 v{download.version}</h3>
          <p role="status">{installing ? "正在启动安装…" : download.message}</p>
          {active && (
            <>
              <progress
                aria-label="安装包下载进度"
                max={download.total || 1}
                value={download.total ? download.downloaded : undefined}
              />
              {!!download.total && (
                <p>
                  {Math.round((download.downloaded / download.total) * 100)}% ·{" "}
                  {(download.downloaded / 1048576).toFixed(1)} /{" "}
                  {(download.total / 1048576).toFixed(1)} MB
                </p>
              )}
            </>
          )}
          {download.phase === "ready" && (
            <p className="scope-hint">
              {release?.installInstructions ||
                "点击安装后，请按系统提示完成更新。"}
              安装期间协议转换会暂时中断，请先结束正在进行的请求。客户端配置会保留。
            </p>
          )}
          <div className="app-update-actions">
            {(active || download.phase === "ready") && (
              <button
                className="button secondary"
                disabled={cancelling || installing}
                onClick={() => void cancelDownload()}
              >
                {cancelling ? "正在取消…" : "取消更新"}
              </button>
            )}
            {download.phase === "ready" && (
              <button
                className="button primary"
                disabled={cancelling || installing}
                onClick={() => void installDownload()}
              >
                {installing ? "正在安装…" : "安装更新"}
              </button>
            )}
            {download.phase === "completed" && (
              <button
                className="button secondary"
                onClick={() => setDownload(null)}
              >
                返回更新方式
              </button>
            )}
          </div>
        </div>
      )}
      {(downloadError || download?.phase === "failed") && (
        <p className="app-update-error" role="alert">
          {downloadError || download?.message}
        </p>
      )}
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
