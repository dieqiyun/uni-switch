import { useState } from "react";
import { explainError, safeDiagnostic } from "../lib/feedback";

export function ErrorFeedback({
  error,
  secrets = [],
  onAction,
}: {
  error: unknown;
  secrets?: string[];
  onAction?: () => void;
}) {
  const [copied, setCopied] = useState(false);
  const info = explainError(error);
  return (
    <div className="error-feedback" role="alert">
      <p>{info.message}</p>
      {onAction && (
        <button type="button" className="text-button" onClick={onAction}>
          {info.action}
        </button>
      )}
      <details>
        <summary>详细信息</summary>
        <pre>{safeDiagnostic(error, secrets)}</pre>
        <button
          type="button"
          className="text-button"
          onClick={async () => {
            try {
              await navigator.clipboard.writeText(
                safeDiagnostic(error, secrets),
              );
              setCopied(true);
            } catch {
              setCopied(false);
            }
          }}
        >
          {copied ? "已复制" : "复制诊断信息"}
        </button>
      </details>
    </div>
  );
}
