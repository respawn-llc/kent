import {
  ArrowUp,
  AlertTriangle,
  Folder,
  GitBranch,
  Goal,
  Settings,
  Square,
  Terminal,
  Zap,
} from "lucide-react";

import { cx, Spinner } from "@/ui";
import "./composerIcon.css";

const icons = {
  send: ArrowUp,
  warning: AlertTriangle,
  workspace: Folder,
  worktree: GitBranch,
  settings: Settings,
  stop: Square,
  goal: Goal,
  processes: Terminal,
  fast: Zap,
};

export function ComposerIcon({
  kind,
  className,
}: Readonly<{ kind: keyof typeof icons | "loading"; className?: string }>) {
  if (kind === "loading") {
    return <Spinner className={cx("chat-composer-icon", className)} size="sm" strokeWidth={2} />;
  }
  const Icon = icons[kind];
  return (
    <Icon
      aria-hidden="true"
      className={cx("chat-composer-icon", `chat-composer-icon--${kind}`, className)}
      nonScalingStroke
      strokeWidth={1.5}
    />
  );
}
