import { useId, type CSSProperties, type ReactNode } from "react";
import { X } from "lucide-react";

import { cx } from "./classes";

export type DialogProps = Readonly<{
  title: string;
  closeLabel: string;
  open: boolean;
  children: ReactNode;
  className?: string;
  style?: CSSProperties;
  onClose: () => void;
}>;

export function Dialog({ title, closeLabel, open, children, className, style, onClose }: DialogProps) {
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
          "island-glass relative grid max-h-[calc(100vh-48px)] w-[min(720px,calc(100vw-32px))] gap-[var(--space-4)] overflow-hidden rounded-[var(--radius-xl)] p-[var(--space-4)]",
          className,
        )}
        role="dialog"
        style={style}
      >
        <header className="flex items-center justify-between gap-[var(--space-4)]">
          <h2 className="m-0 text-[1.15rem] font-bold" id={titleId}>{title}</h2>
          <button
            aria-label={closeLabel}
            className="grid h-9 w-9 place-items-center rounded-full border border-transparent bg-transparent text-[var(--color-on-island)]"
            onClick={onClose}
            type="button"
          >
            <X aria-hidden="true" size={18} strokeWidth={1.5} />
          </button>
        </header>
        <div className="min-h-0 overflow-auto pr-[var(--space-1)] hide-scrollbar">{children}</div>
      </section>
    </div>
  );
}
