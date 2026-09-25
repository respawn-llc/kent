import type { QueryClient } from "@tanstack/react-query";
import { useAtomMount, useAtomSet } from "@effect/atom-react";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import { queryKeys, type SidebarMode, type SidebarRootController } from "@/app-facade";
import { createProjectTaskRefresh } from "./ProjectTaskRefresh";
import { projectTaskGroups } from "./projectTaskListData";
import { projectTaskSortsEqual, type ProjectTaskSort } from "./projectTaskSorting";

type SidebarInput = Readonly<{ open: SidebarRootController["open"]; mode: SidebarMode }>;

export function createProjectTasksModel(
  client: QueryClient,
  projectID: string,
  available: Atom.Atom<boolean>,
) {
  const refresh = createProjectTaskRefresh(client, projectID);
  const creating = Atom.fn<SidebarInput>()(
    (input) =>
      Effect.promise(
        async () =>
          input.open({
            kind: "newTask",
            mode: input.mode,
            projectID,
            boardQueryWorkflowID: undefined,
            onCreated: refresh.rows,
          }).lifecycle,
      ),
    { concurrent: true },
  );
  const newTask = Atom.fn<SidebarInput>()(
    (input, get) =>
      Effect.gen(function* () {
        if (!get(available) || get(creating).waiting) return;
        yield* get.setResult(creating, input);
      }),
    { concurrent: true },
  );
  const linking = Atom.fn<SidebarInput>()(
    (input) =>
      Effect.promise(
        async () =>
          input.open({ kind: "linkWorkflow", mode: input.mode, projectID, onCompleted: refresh.linked })
            .lifecycle,
      ),
    { concurrent: true },
  );
  const linkWorkflow = Atom.fn<SidebarInput>()(
    (input, get) =>
      Effect.gen(function* () {
        if (get(linking).waiting) return;
        yield* get.setResult(linking, input);
      }),
    { concurrent: true },
  );
  const taskDetail = Atom.fn<SidebarInput & Readonly<{ taskID: string; dependencies: boolean }>>()(
    (input) =>
      Effect.sync(() => {
        input.open({
          kind: "taskDetail",
          mode: input.mode,
          taskID: input.taskID,
          ...(input.dependencies ? { initialFocus: { kind: "dependencies" as const } } : {}),
        });
      }),
    { concurrent: true },
  );
  const workflow = Atom.fn<
    Readonly<{ workflowID: string; openProject: (projectID: string, workflowID: string) => Promise<void> }>
  >()((input) => Effect.promise(async () => input.openProject(projectID, input.workflowID)), {
    concurrent: true,
  });
  const sort = Atom.fn<
    Readonly<{
      current: ProjectTaskSort;
      next: ProjectTaskSort;
      apply: (next: ProjectTaskSort) => void;
    }>
  >()(
    (input) =>
      Effect.sync(() => {
        if (projectTaskSortsEqual(input.current, input.next)) return;
        for (const group of projectTaskGroups) {
          client.removeQueries({
            exact: true,
            queryKey: queryKeys.projectTaskGroup(projectID, group, input.next),
          });
        }
        input.apply(input.next);
      }),
    { concurrent: true },
  );
  return {
    available,
    creating,
    newTask,
    linking,
    linkWorkflow,
    taskDetail,
    workflow,
    sort,
    refresh,
  } as const;
}

export function useProjectTasksActions(model: ReturnType<typeof createProjectTasksModel>) {
  useAtomMount(model.available);
  useAtomMount(model.creating);
  useAtomMount(model.linking);
  return {
    newTask: useAtomSet(model.newTask),
    linkWorkflow: useAtomSet(model.linkWorkflow),
    taskDetail: useAtomSet(model.taskDetail),
    workflow: useAtomSet(model.workflow),
    sort: useAtomSet(model.sort),
  };
}
