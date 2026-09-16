import * as Atom from "effect/unstable/reactivity/Atom";
import type { ChatSettingsTarget, WorkspaceCatalogRow } from "@/api";

export type ChatDestinationOpening =
  | Extract<ChatSettingsTarget, { kind: "session" }>
  | Readonly<{ kind: "new_chat"; projectID: string; workspace: WorkspaceCatalogRow }>;

export function createChatDestinationViewModel(opening: ChatDestinationOpening) {
  const selection = Atom.make(opening);
  const target = Atom.make<ChatSettingsTarget>((get) => {
    const selected = get(selection);
    return selected.kind === "session"
      ? selected
      : {
          kind: "new_chat",
          projectID: selected.projectID,
          workspace: { workspaceID: selected.workspace.id },
        };
  });
  return { selection, target } as const;
}
