import * as Dialog from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import type { ReactNode } from "react";

export function Modal({
  title,
  description,
  children,
  onClose,
  wide = false,
  busy = false,
  dismissOnOutside = true,
  initialFocusId,
  className = "",
}: {
  title: string;
  description: string;
  children: ReactNode;
  onClose: () => void;
  wide?: boolean;
  busy?: boolean;
  dismissOnOutside?: boolean;
  initialFocusId?: string;
  className?: string;
}) {
  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open && !busy) onClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Overlay className="modal-overlay" />
        <Dialog.Content
          className={`modal ${wide ? "modal-wide" : ""} ${className}`}
          onOpenAutoFocus={(event) => {
            const initial =
              initialFocusId && document.getElementById(initialFocusId);
            if (initial) {
              event.preventDefault();
              initial.focus();
            }
          }}
          onEscapeKeyDown={(event) => {
            if (busy) event.preventDefault();
          }}
          onInteractOutside={(event) => {
            if (busy || !dismissOnOutside) event.preventDefault();
          }}
          onCloseAutoFocus={(event) => {
            event.preventDefault();
            const trigger =
              document.querySelector<HTMLElement>("[data-dialog-return]") ||
              document.querySelector<HTMLElement>("[data-dialog-fallback]") ||
              document.querySelector<HTMLElement>(
                '[role="tab"][data-state="active"]',
              );
            trigger?.focus();
          }}
        >
          <div className="modal-heading">
            <div>
              <Dialog.Title>{title}</Dialog.Title>
              <Dialog.Description>{description}</Dialog.Description>
            </div>
            <Dialog.Close asChild>
              <button
                className="icon-button"
                aria-label="关闭弹窗"
                disabled={busy}
              >
                <X size={19} aria-hidden />
              </button>
            </Dialog.Close>
          </div>
          {children}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
