import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import type { SidebarRootController } from "@/app-facade";

export type TaskSearchInvocation =
  Readonly<{ projectId: string; onOpenTask(taskID: string): void }> | Readonly<{ projectId?: never }>;

export function createBoardSearchModel() {
  const state = Atom.make<Readonly<{ invocation: TaskSearchInvocation | null; open: boolean }>>({
    invocation: null,
    open: false,
  });
  const openSearch = Atom.fn<TaskSearchInvocation>()((invocation, get) =>
    Effect.sync(() => {
      get.set(state, { invocation, open: true });
    }),
  );
  const close = Atom.fn((_, get) =>
    Effect.sync(() => {
      get.set(state, { ...get(state), open: false });
    }),
  );
  const cancelProjectSearch = Atom.fn<string>()((projectID, get) =>
    Effect.sync(() => {
      if (get(state).invocation?.projectId === projectID) get.set(state, { invocation: null, open: false });
    }),
  );
  const activate = Atom.fn<
    Readonly<{
      invocation: TaskSearchInvocation;
      taskID: string;
      openSidebar: SidebarRootController["open"];
    }>
  >()(
    (input) =>
      Effect.sync(() => {
        if (input.invocation.projectId !== undefined) {
          input.invocation.onOpenTask(input.taskID);
        } else {
          input.openSidebar({ kind: "taskDetail", mode: "overlay", taskID: input.taskID });
        }
      }),
    { concurrent: true },
  );
  const observation: Atom.Atom<Atom.Type<typeof state>> = state;
  return {
    state: observation,
    openSearch,
    close,
    cancelProjectSearch,
    activate,
  } as const;
}
