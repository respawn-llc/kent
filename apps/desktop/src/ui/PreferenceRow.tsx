import type { ReactNode } from "react";
import { DisabledInteractionGuard } from "./DisabledInteractionGuard";

export function PreferenceRow({
  label,
  disabledReason,
  children,
}: Readonly<{
  label: string;
  disabledReason?: string | undefined;
  children: ReactNode;
}>) {
  return (
    <DisabledInteractionGuard
      disabled={disabledReason !== undefined}
      reason={disabledReason}
      className="flex min-w-0 flex-wrap items-center justify-between gap-[var(--space-3)] py-[var(--space-2)]"
    >
      <span>{label}</span>
      {children}
    </DisabledInteractionGuard>
  );
}
