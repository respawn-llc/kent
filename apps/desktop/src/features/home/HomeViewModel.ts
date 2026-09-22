import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import type { HomeSidebarCategory } from "./HomeSidebar";
import type { QueryClient } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import {
  taskDetailInitialFocusFromAttentionItem,
  type AppServices,
  type StatusController,
  type SidebarMode,
  type SidebarRootController,
  type SessionChatTarget,
} from "@/app-facade";
import type { AttentionItem } from "@/api";
import { createHomeAttentionPages, createHomeProjectPages } from "./useHomeData";
import { createProjectCreationModel } from "./ProjectCreationModel";
import { createHomeCreationObservation } from "./HomeCreationObservation";

export function createHomeViewModel(
  input: Readonly<{
    services: AppServices;
    client: QueryClient;
    t: TFunction;
    push: StatusController["push"];
    openProject: (projectID: string) => Promise<void>;
  }>,
) {
  const projects = createHomeProjectPages(input.services.api, input.client);
  const attention = createHomeAttentionPages(input.services.api, input.client, false);
  const creation = createProjectCreationModel(input);
  const creationObservation = createHomeCreationObservation(input);
  const projectItems = Atom.make(
    (get) => get(projects.request).data?.pages.flatMap((page) => page.projects) ?? [],
  );
  const attentionItems = Atom.make(
    (get) => get(attention.request).data?.pages.flatMap((page) => page.items) ?? [],
  );
  const category = Atom.make<HomeSidebarCategory>("projects");
  const selectCategory = Atom.fn<
    Readonly<{
      category: HomeSidebarCategory;
      selectedProjectID: string | null;
      selectProject: (projectID: string | null) => Promise<void>;
    }>
  >()(
    (input, get) =>
      Effect.gen(function* () {
        if (get(category) === input.category && input.selectedProjectID === null) return;
        get.set(category, input.category);
        if (input.selectedProjectID !== null) yield* Effect.promise(async () => input.selectProject(null));
      }),
    { concurrent: true },
  );
  const selectProject = Atom.fn<
    Readonly<{
      projectID: string;
      selectedProjectID: string | null;
      selectProject: (projectID: string | null) => Promise<void>;
    }>
  >()(
    (input) =>
      Effect.promise(async () =>
        input.selectProject(input.projectID === input.selectedProjectID ? null : input.projectID),
      ),
    { concurrent: true },
  );
  const createWorkflow = Atom.fn<Readonly<{ open: SidebarRootController["open"]; mode: SidebarMode }>>()(
    (input) =>
      Effect.sync(() => {
        input.open({ kind: "workflowCreate", mode: input.mode });
      }),
    { concurrent: true },
  );
  const attentionTask = Atom.fn<
    Readonly<{
      item: AttentionItem;
      open: SidebarRootController["open"];
      mode: SidebarMode;
    }>
  >()(
    (input) =>
      Effect.sync(() => {
        input.open({
          kind: "taskDetail",
          initialFocus: taskDetailInitialFocusFromAttentionItem(input.item),
          inboxNav: true,
          mode: input.mode,
          onMutated: undefined,
          taskID: input.item.taskID,
        });
      }),
    { concurrent: true },
  );
  const attentionChat = Atom.fn<
    Readonly<{
      target: SessionChatTarget;
      open: (target: SessionChatTarget) => Promise<void>;
    }>
  >()((input) => Effect.promise(async () => input.open(input.target)), { concurrent: true });
  return {
    projects,
    attention,
    creation,
    creationObservation,
    projectItems,
    attentionItems,
    category: Atom.make((get) => get(category)),
    selectCategory,
    selectProject,
    createWorkflow,
    attentionTask,
    attentionChat,
  } as const;
}

export type HomeViewModel = ReturnType<typeof createHomeViewModel>;
