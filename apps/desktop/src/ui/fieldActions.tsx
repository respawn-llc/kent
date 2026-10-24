import { Children, type ReactNode, type ReactElement } from "react";

export function fieldActions(...actions: ReactNode[]): ReactElement | undefined {
  const present = Children.toArray(actions);
  return present.length === 0 ? undefined : (
    <div className="flex items-center gap-[var(--space-2)]">{present}</div>
  );
}
