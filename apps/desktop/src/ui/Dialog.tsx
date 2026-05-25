import { useId, type CSSProperties, type ReactNode } from "react";
import { X } from "lucide-react";

import { cx } from "./classes";
import { chromeContentPaddingClassName } from "./chromePadding";

export type DialogProps = Readonly<{
  title: string;
  closeLabel: string;
  open: boolean;
  children: ReactNode;
  className?: string;
  chrome?: "header" | "floating-close";
  contentPadding?: "none" | "chrome";
  surface?: "island" | "transparent";
  style?: CSSProperties;
  onClose: () => void;
}>;

export function Dialog({
  title,
  closeLabel,
  open,
  children,
  className,
  chrome = "header",
  contentPadding = "none",
  surface = "island",
  style,
  onClose,
}: DialogProps) {
  const titleId = useId();

  if (!open) {
    return null;
  }

  return (
    <div
      className="app-region-no-drag fixed inset-0 z-50 grid place-items-center p-[var(--space-4)]"
      role="presentation"
    >
      <div
        className="absolute inset-0 bg-black/35 backdrop-blur-[6px]"
        onClick={onClose}
        role="presentation"
      />
      <section
        aria-labelledby={titleId}
        aria-modal="true"
        className={cx(
          "relative grid max-h-[calc(100vh-48px)] w-[min(720px,calc(100vw-32px))] gap-[var(--space-4)] overflow-hidden",
          surface === "island" && "island-glass rounded-[var(--radius-xl)] p-[var(--space-4)]",
          surface === "transparent" && "bg-transparent p-0 shadow-none",
          className,
        )}
        role="dialog"
        style={style}
      >
        {chrome === "header" ? (
          <header className="flex items-center justify-between gap-[var(--space-4)]">
            <h2 className="m-0 text-[1.15rem] font-bold" id={titleId}>
              {title}
            </h2>
            <DialogCloseButton closeLabel={closeLabel} onClose={onClose} />
          </header>
        ) : (
          <>
            <h2 className="sr-only" id={titleId}>
              {title}
            </h2>
            <DialogCloseButton
              className="absolute top-[var(--space-3)] right-[var(--space-3)] z-10 bg-[var(--color-island-1)]"
              closeLabel={closeLabel}
              onClose={onClose}
            />
          </>
        )}
        <div
          className={cx(
            "min-h-0 overflow-auto hide-scrollbar",
            contentPadding === "none" && "pr-[var(--space-1)]",
            contentPadding === "chrome" && chromeContentPaddingClassName,
          )}
        >
          {children}
        </div>
      </section>
    </div>
  );
}

function DialogCloseButton({
  className,
  closeLabel,
  onClose,
}: Readonly<{ className?: string | undefined; closeLabel: string; onClose: () => void }>) {
  return (
    <button
      aria-label={closeLabel}
      className={cx(
        "grid h-9 w-9 place-items-center rounded-full border border-transparent bg-transparent text-[var(--color-on-island)]",
        className,
      )}
      onClick={onClose}
      type="button"
    >
      <X aria-hidden="true" size={18} strokeWidth={1.5} />
    </button>
  );
}
