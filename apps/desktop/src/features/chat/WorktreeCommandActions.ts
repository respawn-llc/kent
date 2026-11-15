import type { SidebarRootController, ChatWorktreeBinding } from "@/app-facade";
import { createWorktreeActions } from "./WorktreeActions";
import type { WorktreeCommandIntent } from "./worktreeCommand";
import * as Atom from "effect/reactivity/Atom";

export function createWorktreeCommandActions({
  roots,
  focusComposer,
  openSession,
  deleteTarget,
  chat,
  ...dependencies
}: Omit<Parameters<typeof createWorktreeActions>[0], "onAccepted" | "sessionID"> &
  Readonly<{
    chat: ChatWorktreeBinding;
    roots: Pick<SidebarRootController, "open">;
    focusComposer(): void;
    projectID: string;
    openSession(sessionID: string): void | Promise<void>;
    deleteTarget(selector: string | null): Promise<void>;
  }>) {
  return Atom.make((get) => {
    const scope = get(chat.scope);
    if (scope === null) return null;
    const sessionID = scope.kind === "session" ? scope.sessionID : null;
    const transitions = createWorktreeActions({ ...dependencies, sessionID, onAccepted: () => undefined });
    const execute = async (intent: WorktreeCommandIntent) => {
      switch (intent.kind) {
        case "list":
        case "create": {
          const handle = roots.open({
            kind: "worktree",
            projectID: dependencies.projectID,
            chat,
            page: intent.kind,
            openSession,
          });
          void handle.lifecycle.then((outcome) => {
            if (outcome === "closed") focusComposer();
          });
          return;
        }
        case "switch":
          if (sessionID === null) {
            chat.select(intent.selector);
            return;
          }
          return transitions.submitSwitch({ kind: "selector", selector: intent.selector });
        case "leave":
          if (sessionID === null) {
            chat.select(null);
            return;
          }
          return transitions.submitSwitch({ kind: "leave" });
        case "delete":
          return deleteTarget(intent.selector);
      }
    };
    return { transitions, execute };
  });
}
