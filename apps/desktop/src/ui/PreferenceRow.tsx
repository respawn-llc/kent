import type { ReactNode } from "react";
import { DisabledInteractionGuard } from "./DisabledInteractionGuard";
import { HelpHint } from "./HelpHint";

export function PreferenceRow({
  label,
  help,
  disabled = false,
  disabledReason,
  children,
}: Readonly<{
  label: string;
  help?: string;
  disabled?: boolean;
  disabledReason?: string | undefined;
  children: ReactNode;
}>) {
  return (
    <DisabledInteractionGuard
      disabled={disabled || disabledReason !== undefined}
      reason={disabledReason}
      className="flex min-w-0 flex-wrap items-center justify-between gap-[var(--space-3)] py-[var(--space-1)]"
    >
      <span className="inline-flex min-w-0 items-center gap-[var(--space-2)] text-sm font-medium">
        {label}
        {help !== undefined && <HelpHint label={help} side="right" />}
      </span>
      {children}
    </DisabledInteractionGuard>
  );
}
