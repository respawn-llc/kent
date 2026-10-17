import type { SessionChatOpeningIntent, SidebarRootController } from "@/app-facade";
import { createWorktreeActions } from "./WorktreeActions";
import type { WorktreeCommandIntent } from "./worktreeCommand";

export function createWorktreeCommandActions({
  roots,
  focusComposer,
  openSession,
  deleteTarget,
  ...dependencies
}: Omit<Parameters<typeof createWorktreeActions>[0], "onAccepted"> &
  Readonly<{
    roots: Pick<SidebarRootController, "open">;
    focusComposer(): void;
    openSession(sessionID: string, intent?: SessionChatOpeningIntent): void | Promise<void>;
    deleteTarget(selector: string | null): Promise<void>;
  }>) {
  const transitions = createWorktreeActions({ ...dependencies, onAccepted: () => undefined });
  const execute = async (intent: WorktreeCommandIntent) => {
    switch (intent.kind) {
      case "list":
      case "create": {
        const handle = roots.open({
          kind: "worktree",
          sessionID: dependencies.sessionID,
          page: intent.kind,
          openSession,
        });
        void handle.lifecycle.then((outcome) => {
          if (outcome === "closed") focusComposer();
        });
        return;
      }
      case "switch":
        return transitions.submitSwitch({ kind: "selector", selector: intent.selector });
      case "leave":
        return transitions.submitSwitch({ kind: "leave" });
      case "delete":
        return deleteTarget(intent.selector);
    }
  };
  return { transitions, execute };
}
