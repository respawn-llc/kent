import type { MutationOptions, QueryClient } from "@tanstack/react-query";
import * as Atom from "effect/unstable/reactivity/Atom";

type TaskRequestGroup = "start-move" | "resume" | "interrupt";

class TaskRequest {
  constructor(
    readonly owner: symbol,
    readonly taskID: string,
    readonly group: TaskRequestGroup,
  ) {}
}

export function createTaskRequests(client: QueryClient) {
  const owner = Symbol("Task requests");
  const cache = client.getMutationCache();
  const active = () =>
    cache.findAll({
      status: "pending",
      predicate: (mutation) => {
        const input = mutation.state.variables;
        return input instanceof TaskRequest && input.owner === owner;
      },
    });
  const snapshot = (): Readonly<{
    startMove: ReadonlySet<string>;
    resume: ReadonlySet<string>;
    interrupt: ReadonlySet<string>;
  }> => {
    const startMove = new Set<string>();
    const resume = new Set<string>();
    const interrupt = new Set<string>();
    for (const mutation of active()) {
      const input = mutation.state.variables;
      if (!(input instanceof TaskRequest)) throw new Error("Task request identity is missing.");
      switch (input.group) {
        case "start-move":
          startMove.add(input.taskID);
          break;
        case "resume":
          resume.add(input.taskID);
          break;
        case "interrupt":
          interrupt.add(input.taskID);
          break;
      }
    }
    return { startMove, resume, interrupt } as const;
  };
  const pending = Atom.make((get) => {
    get.addFinalizer(
      cache.subscribe((event) => {
        const input: unknown = event.mutation?.state.variables;
        if (input instanceof TaskRequest && input.owner === owner) get.setSelf(snapshot());
      }),
    );
    return snapshot();
  });
  function start<A>(
    taskID: string,
    group: TaskRequestGroup,
    options: MutationOptions<A, unknown, TaskRequest>,
  ): Promise<A> | null {
    if (
      active().some((mutation) => {
        const input = mutation.state.variables;
        return input instanceof TaskRequest && input.taskID === taskID && input.group === group;
      })
    )
      return null;
    return cache.build(client, options).execute(new TaskRequest(owner, taskID, group));
  }
  return { pending, start } as const;
}
