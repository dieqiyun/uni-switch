import { useEffect, useRef, useState } from "react";
import { getCurrentWindow } from "@tauri-apps/api/window";
import { Copy, Minus, Square, X } from "lucide-react";
import { desktopRuntime } from "../lib/api";

// Keep native window access out of browser previews.
export function WindowChrome() {
  return desktopRuntime ? <DesktopWindowChrome /> : null;
}

function DesktopWindowChrome() {
  const [maximized, setMaximized] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const pending = useRef(false);

  useEffect(() => {
    const nativeWindow = getCurrentWindow();
    let disposed = false;
    let unlisten: (() => void) | undefined;
    let revision = 0;
    const refresh = async () => {
      const current = ++revision;
      try {
        const value = await nativeWindow.isMaximized();
        if (!disposed && current === revision) setMaximized(value);
      } catch {
        // A later resize retries the read; controls remain usable.
      }
    };
    void nativeWindow
      .onResized(() => void refresh())
      .then(
        (stop) => {
          if (disposed) stop();
          else {
            unlisten = stop;
            void refresh();
          }
        },
        () => void refresh(),
      );
    return () => {
      disposed = true;
      unlisten?.();
    };
  }, []);

  async function operate(action: "minimize" | "maximize" | "close") {
    if (pending.current) return;
    pending.current = true;
    setBusy(true);
    setError(null);
    try {
      const nativeWindow = getCurrentWindow();
      if (action === "minimize") await nativeWindow.minimize();
      else if (action === "maximize") {
        await nativeWindow.toggleMaximize();
        setMaximized(await nativeWindow.isMaximized());
      } else {
        // CloseRequested hides the window and preserves the tray and bridge.
        await nativeWindow.close();
      }
    } catch {
      setError("窗口操作未完成，请重试。");
    } finally {
      pending.current = false;
      setBusy(false);
    }
  }

  const maximizeLabel = maximized ? "还原窗口" : "最大化窗口";
  return (
    <>
      <div className="window-chrome" role="region" aria-label="窗口控制">
        <div className="window-drag-region" data-tauri-drag-region />
        <div className="window-controls" role="group" aria-label="窗口操作">
          <button
            type="button"
            aria-label="最小化窗口"
            title="最小化"
            disabled={busy}
            onClick={() => void operate("minimize")}
          >
            <Minus size={15} aria-hidden />
          </button>
          <button
            type="button"
            aria-label={maximizeLabel}
            title={maximized ? "还原" : "最大化"}
            disabled={busy}
            onClick={() => void operate("maximize")}
          >
            {maximized ? (
              <Copy size={13} aria-hidden />
            ) : (
              <Square size={13} aria-hidden />
            )}
          </button>
          <button
            type="button"
            className="window-close"
            aria-label="关闭窗口并留在托盘"
            title="关闭窗口，继续在托盘运行"
            disabled={busy}
            onClick={() => void operate("close")}
          >
            <X size={17} aria-hidden />
          </button>
        </div>
        {error && (
          <div className="window-operation-error" role="alert">
            {error}
          </div>
        )}
      </div>
    </>
  );
}
