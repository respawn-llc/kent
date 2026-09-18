import { useAtomMount, useAtomSet } from "@effect/atom-react";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import { useMemo } from "react";
import {
  useAppNavigation,
  useOwnedSidebarRoots,
  type AppNavigation,
  type SidebarRootController,
} from "@/app-facade";
import { completeBoardWorkflowLink } from "./boardWorkflowLinkCompletion";

type Context = Readonly<{
  navigation: AppNavigation;
  open: SidebarRootController["open"];
  report(error: unknown): void;
}>;

const navigate = (operation: () => Promise<unknown>, report: Context["report"]) =>
  Effect.tryPromise({
    try: operation,
    catch: (cause) => ({ _tag: "BoardNavigationError" as const, cause }),
  }).pipe(
    Effect.catch((error) =>
      Effect.sync(() => {
        report(error.cause);
      }),
    ),
  );

function createBoardNavigation(projectID: string, workflowID: string) {
  const openTask = Atom.fn<Context & Readonly<{ taskID: string }>>()(
    (input) =>
      navigate(
        async () => input.navigation.openProjectTask(projectID, workflowID, input.taskID),
        input.report,
      ),
    { concurrent: true },
  );
  const selectWorkflow = Atom.fn<Context & Readonly<{ workflowID: string }>>()(
    (input) => navigate(async () => input.navigation.openProject(projectID, input.workflowID), input.report),
    { concurrent: true },
  );
  const openTasks = Atom.fn<Context>()(
    (input) => navigate(async () => input.navigation.openProjectTasks(projectID), input.report),
    { concurrent: true },
  );
  const openDependencies = Atom.fn<Context & Readonly<{ taskID: string }>>()(
    (input) =>
      Effect.sync(() => {
        input.open({
          kind: "taskDetail",
          initialFocus: { kind: "dependencies" },
          mode: "overlay",
          taskID: input.taskID,
        });
      }),
    { concurrent: true },
  );
  const openNewTask = Atom.fn<Context & Readonly<{ boardQueryWorkflowID: string | undefined }>>()(
    (input) =>
      Effect.sync(() => {
        input.open({
          kind: "newTask",
          mode: "overlay",
          projectID,
          workflowID,
          boardQueryWorkflowID: input.boardQueryWorkflowID,
        });
      }),
    { concurrent: true },
  );
  const openLinkWorkflow = Atom.fn<Context>()(
    (input) =>
      Effect.sync(() => {
        input.open({
          kind: "linkWorkflow",
          mode: "overlay",
          projectID,
          selectedWorkflowID: workflowID,
          onCompleted: async (completion) =>
            completeBoardWorkflowLink(input.navigation, projectID, completion),
        });
      }),
    { concurrent: true },
  );
  return { openTask, selectWorkflow, openTasks, openDependencies, openNewTask, openLinkWorkflow } as const;
}

export function useBoardNavigation(
  projectID: string,
  workflowID: string,
  input: Readonly<{
    boardQueryWorkflowID: string | undefined;
    selectedTaskID: string;
    report(error: unknown): void;
  }>,
) {
  const navigation = useAppNavigation();
  const { open } = useOwnedSidebarRoots();
  const model = useMemo(() => createBoardNavigation(projectID, workflowID), [projectID, workflowID]);
  const { selectedTaskID, report, boardQueryWorkflowID } = input;
  const selectedTask = useMemo(
    () =>
      Atom.make(
        Effect.gen(function* () {
          if (selectedTaskID.length === 0) return;
          const root = yield* Effect.acquireRelease(
            Effect.sync(() =>
              open({ kind: "taskDetail", mode: "overlay", onMutated: undefined, taskID: selectedTaskID }),
            ),
            (handle) =>
              Effect.sync(() => {
                handle.release();
              }),
          );
          const outcome = yield* Effect.promise(async () => root.lifecycle);
          if (outcome === "closed") {
            yield* navigate(async () => navigation.closeProjectTask(projectID, workflowID), report);
          }
        }),
      ),
    [selectedTaskID, open, navigation, projectID, workflowID, report],
  );
  useAtomMount(selectedTask);
  const openTask = useAtomSet(model.openTask, { mode: "value" });
  const selectWorkflow = useAtomSet(model.selectWorkflow, { mode: "value" });
  const openTasks = useAtomSet(model.openTasks, { mode: "value" });
  const openDependencies = useAtomSet(model.openDependencies, { mode: "value" });
  const openNewTask = useAtomSet(model.openNewTask, { mode: "value" });
  const openLinkWorkflow = useAtomSet(model.openLinkWorkflow, { mode: "value" });
  const context = { navigation, open, report };
  return {
    openTask: (taskID: string) => {
      openTask({ ...context, taskID });
    },
    selectWorkflow: (target: string) => {
      selectWorkflow({ ...context, workflowID: target });
    },
    openTasks: () => {
      openTasks(context);
    },
    openDependencies: (taskID: string) => {
      openDependencies({ ...context, taskID });
    },
    openNewTask: () => {
      openNewTask({ ...context, boardQueryWorkflowID });
    },
    openLinkWorkflow: () => {
      openLinkWorkflow(context);
    },
  };
}

export function useCloseBoardTask(
  projectID: string,
  workflowID: string | undefined,
  report: Context["report"],
) {
  const navigation = useAppNavigation();
  const action = useMemo(
    () =>
      Atom.fn<Readonly<{ navigation: AppNavigation; report: Context["report"] }>>()(
        (input) =>
          navigate(async () => input.navigation.closeProjectTask(projectID, workflowID), input.report),
        { concurrent: true },
      ),
    [projectID, workflowID],
  );
  const close = useAtomSet(action, { mode: "value" });
  return {
    close: () => {
      close({ navigation, report });
    },
  };
}
