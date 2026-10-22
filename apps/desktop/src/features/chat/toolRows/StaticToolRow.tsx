import type { ReactNode } from "react";

import { cx, type TranscriptDisclosureIconTone } from "@/ui";

export function StaticToolRow({
  compact,
  icon,
  iconTone,
  actions,
}: Readonly<{
  compact: string | null;
  icon: ReactNode;
  iconTone: TranscriptDisclosureIconTone;
  actions?: ReactNode;
}>) {
  return (
    <div
      className={cx(
        "transcript-tool-static-row min-h-9",
        actions !== undefined && "transcript-tool-static-row--actions",
      )}
      data-transcript-collapsed
    >
      <span
        aria-hidden="true"
        className={cx("transcript-tool-static-icon size-5", `transcript-disclosure-icon--${iconTone}`)}
      >
        {icon}
      </span>
      <span className="min-w-0 truncate text-sm text-[var(--color-muted)]">{compact}</span>
      {actions}
    </div>
  );
}
