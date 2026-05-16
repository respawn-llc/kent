import type { ReactNode } from "react";

import { Button } from "./Button";
import { cx } from "./classes";

export type DialogProps = Readonly<{
  title: string;
  closeLabel: string;
  open: boolean;
  children: ReactNode;
  className?: string;
  onClose: () => void;
}>;

export function Dialog({ title, closeLabel, open, children, className, onClose }: DialogProps) {
  if (!open) {
    return null;
  }

  return (
    <div className="ui-dialog-shell" role="presentation">
      <div className="ui-dialog-backdrop" onClick={onClose} role="presentation" />
      <section aria-modal="true" className={cx("ui-dialog", className)} role="dialog">
        <header className="ui-dialog__header">
          <h2>{title}</h2>
          <Button aria-label={closeLabel} onClick={onClose} variant="ghost">
            {closeLabel}
          </Button>
        </header>
        <div className="ui-dialog__content">{children}</div>
      </section>
    </div>
  );
}
