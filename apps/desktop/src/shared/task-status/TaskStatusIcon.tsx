import { CheckCircle2, Circle, CircleDot, CircleHelp, CirclePause, CircleStop } from "lucide-react";
import type { ReactNode } from "react";

import type { TaskStatusKind } from "@/api";
import { Spinner } from "@/ui";

export function TaskStatusIcon({ status }: Readonly<{ status: TaskStatusKind }>): ReactNode {
  switch (status) {
    case "done":
      return <CheckCircle2 aria-hidden="true" className="shrink-0 text-[var(--color-success)]" size={15} />;
    case "backlog":
      return <Circle aria-hidden="true" className="shrink-0" size={15} />;
    case "active":
      return <CircleDot aria-hidden="true" className="shrink-0 text-[var(--color-primary)]" size={15} />;
    case "running":
      return <Spinner className="size-[15px] shrink-0" size="sm" strokeWidth={2} />;
    case "queued":
      return <CirclePause aria-hidden="true" className="shrink-0 text-[var(--color-warning)]" size={15} />;
    case "waiting_approval":
      return <CircleDot aria-hidden="true" className="shrink-0 text-[var(--color-secondary)]" size={15} />;
    case "interrupted":
      return <CircleStop aria-hidden="true" className="shrink-0 text-[var(--color-error)]" size={15} />;
    case "waiting_question":
      return <CircleHelp aria-hidden="true" className="shrink-0 text-[var(--color-primary)]" size={15} />;
  }
}
