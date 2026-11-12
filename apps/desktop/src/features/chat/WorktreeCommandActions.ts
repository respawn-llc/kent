import type { SidebarRootController, ChatWorktreeBinding } from "@/app-facade";
import { createWorktreeActions } from "./WorktreeActions";
import type { WorktreeCommandIntent } from "./worktreeCommand";

export function createWorktreeCommandActions({
  roots,
  focusComposer,
  openSession,
  deleteTarget,
  chat,
  ...dependencies
}: Omit<Parameters<typeof createWorktreeActions>[0], "onAccepted" | "sessionID"> &
  Readonly<{
    sessionID: string | null;
    chat?: ChatWorktreeBinding;
    roots: Pick<SidebarRootController, "open">;
    focusComposer(): void;
    projectID: string;
    openSession(sessionID: string): void | Promise<void>;
    deleteTarget(selector: string | null): Promise<void>;
  }>) {
  const transitions = createWorktreeActions({ ...dependencies, onAccepted: () => undefined });
  const execute = async (intent: WorktreeCommandIntent) => {
    switch (intent.kind) {
      case "list":
      case "create": {
        const handle = roots.open({
          kind: "worktree",
          projectID: dependencies.projectID,
          sessionID: dependencies.sessionID,
          ...(chat === undefined ? {} : { chat }),
          page: intent.kind,
          openSession,
        });
        void handle.lifecycle.then((outcome) => {
          if (outcome === "closed") focusComposer();
        });
        return;
      }
      case "switch":
        if (dependencies.sessionID === null) return chat?.select(intent.selector);
        return transitions.submitSwitch({ kind: "selector", selector: intent.selector });
      case "leave":
        if (dependencies.sessionID === null) return chat?.select(null);
        return transitions.submitSwitch({ kind: "leave" });
      case "delete":
        return deleteTarget(intent.selector);
    }
  };
  return { transitions, execute };
}
