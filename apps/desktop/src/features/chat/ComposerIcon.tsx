import {
  Send,
  ClockArrowRight,
  AlertTriangle,
  Folder,
  GitBranch,
  Goal,
  MessageCircleX,
  Settings,
  Square,
  Terminal,
  Zap,
} from "lucide-react";

import { cx, Spinner } from "@/ui";

const icons = {
  send: Send,
  queue: ClockArrowRight,
  warning: AlertTriangle,
  workspace: Folder,
  worktree: GitBranch,
  settings: Settings,
  stop: Square,
  decline: MessageCircleX,
  goal: Goal,
  processes: Terminal,
  fast: Zap,
};

export function ComposerIcon({
  kind,
  className,
}: Readonly<{ kind: keyof typeof icons | "loading"; className?: string }>) {
  if (kind === "loading") {
    return <Spinner className={className} size="sm" strokeWidth={2} />;
  }
  const Icon = icons[kind];
  return (
    <Icon
      aria-hidden="true"
      className={cx("shrink-0", kind === "fast" ? "size-[1em]" : "size-4", className)}
      nonScalingStroke
      strokeWidth={1.5}
    />
  );
}
