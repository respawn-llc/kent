import { useState } from "react";
import { useAtomMount, useAtomSet, useAtomValue } from "@effect/atom-react";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import type { AttentionItem } from "@/api";
import { errorMessage } from "@/api";
import { useAppNavigation } from "@/app-facade";
import { SidebarRootOwner, useOwnedSidebarRoots, type SidebarMode } from "@/app-facade";
import { useAppServices } from "@/app-facade";
import { useStatusController } from "@/app-facade";
import { desktopChatEnabled } from "@/shared/feature-flags";
import {
  ErrorState,
  LoadingState,
  VirtualizedInfiniteList,
  useStableCallback,
} from "@/ui";
import { HomeSidebar } from "./HomeSidebar";
import { createHomeViewModel, type HomeViewModel } from "./HomeViewModel";
import { HomeProjectContent } from "./HomeProjectContent";
import { OverlappingCrossfade } from "./OverlappingCrossfade";
import { ProjectCreateDialog, type ProjectDraft } from "./ProjectCreateForm";
import { useHomeSidebarMode } from "./useHomeSidebarMode";
import { useGlobalAttentionPages, useProjectPages } from "./useHomeData";
import { AttentionRow } from "./AttentionRow";
import { useProjectCreationActions } from "./ProjectCreationModel";

export function HomeRoute({ selectedProjectID }: Readonly<{ selectedProjectID: string | null }>) {
  return (
    <SidebarRootOwner>
      <HomeRouteContent selectedProjectID={selectedProjectID} />
    </SidebarRootOwner>
  );
}

function HomeRouteContent({ selectedProjectID }: Readonly<{ selectedProjectID: string | null }>) {
  const { t } = useTranslation();
  const services = useAppServices();
  const { push } = useStatusController();
  const { mainPaneRef, sidebarMode } = useHomeSidebarMode();
  const navigation = useAppNavigation();
  const { open } = useOwnedSidebarRoots();
  const queryClient = useQueryClient();
  const openProject = useStableCallback(navigation.openProject);
  const [model] = useState(() =>
    createHomeViewModel({ services, client: queryClient, t, push, openProject }),
  );
  const creation = useAtomValue(model.creation.state);
  const creationActions = useProjectCreationActions(model.creation);
  const projects = useProjectPages(model.projects);
  const attention = useGlobalAttentionPages(model.attention);
  const category = useAtomValue(model.category);
  const selectCategory = useAtomSet(model.selectCategory);
  const selectProject = useAtomSet(model.selectProject);
  const createWorkflow = useAtomSet(model.createWorkflow);
  const projectItems = useAtomValue(model.projectItems);
  const attentionItems = useAtomValue(model.attentionItems);
  const [draft, setDraft] = useState<ProjectDraft | null>(null);
  const closeCreation = () => {
    setDraft(null);
  };
  const projectCreationDialog =
    draft === null ? null : (
      <ProjectCreateDialog
        creationError={creation.error}
        draft={draft}
        isCreating={creation.isPending}
        onClose={closeCreation}
        onSubmitDraft={(draft) => {
          creationActions.submit({
            draft,
            complete: async (projectID) => {
              closeCreation();
              await navigation.openProject(projectID);
            },
            selectionRequired: closeCreation,
          });
        }}
      />
    );

  useAtomMount(model.creationObservation);

  const selectedCategory = selectedProjectID === null ? category : "projects";
  const detailKey = selectedProjectID === null ? "inbox" : `project:${selectedProjectID}`;
  return (
    <div className="h-full min-h-0" data-testid="home-route-root">
      {projectCreationDialog}
      <div className="grid h-full min-h-0 grid-cols-[350px_minmax(0,1fr)]" data-testid="home-pane-grid">
        <HomeSidebar
          onChooseWorkspace={() => {
            creationActions.chooseWorkspace({
              openProject: navigation.openProject,
              openDraft: setDraft,
            });
          }}
          onCreateWorkflow={() => {
            createWorkflow({ open, mode: sidebarMode });
          }}
          onProjectSelect={(projectID) => {
            selectProject({ projectID, selectedProjectID, selectProject: navigation.selectHomeProject });
          }}
          onCategorySelect={(nextCategory) => {
            selectCategory({
              category: nextCategory,
              selectedProjectID,
              selectProject: navigation.selectHomeProject,
            });
          }}
          selectedCategory={selectedCategory}
          projectItems={projectItems}
          projectsQuery={projects}
          sidebarMode={sidebarMode}
          selectedProjectID={selectedProjectID}
        />
        <section
          className="island-glass my-[var(--space-2)] mr-[var(--space-2)] min-h-0 overflow-hidden rounded-[var(--radius-xl)]"
          data-sidebar-protected-main
          ref={mainPaneRef}
        >
          <OverlappingCrossfade contentKey={detailKey}>
            {selectedProjectID !== null ? (
              <HomeProjectContent
                key={selectedProjectID}
                projectID={selectedProjectID}
                sessionsVisible={desktopChatEnabled}
                sidebarMode={sidebarMode}
              />
            ) : (
              <AttentionList
                items={attentionItems}
                model={model}
                query={attention}
                sidebarMode={sidebarMode}
              />
            )}
          </OverlappingCrossfade>
        </section>
      </div>
    </div>
  );
}

type AttentionListProps = Readonly<{
  items: readonly AttentionItem[];
  query: ReturnType<typeof useGlobalAttentionPages>;
  sidebarMode: SidebarMode;
  model: HomeViewModel;
}>;

function AttentionList({ items, query, sidebarMode, model }: AttentionListProps) {
  const { t } = useTranslation();
  const { open } = useOwnedSidebarRoots();
  const navigation = useAppNavigation();
  const openTask = useAtomSet(model.attentionTask);
  const openChat = useAtomSet(model.attentionChat);
  const onTaskDetail = useStableCallback((item: AttentionItem) => {
    openTask({ item, open, mode: sidebarMode });
  });
  const onSessionChat = useStableCallback((target: Parameters<typeof navigation.openSessionChat>[0]) => {
    openChat({ target, open: navigation.openSessionChat });
  });
  if (query.isPending) {
    return <LoadingState appearanceDelayMs={0} fullPage={false} reveal={false} title={t("states.loading")} />;
  }
  if (query.isError) {
    return <ErrorState body={errorMessage(query.error)} reveal={false} title={t("states.error")} />;
  }
  return (
    <VirtualizedInfiniteList
      className="h-full min-h-0 overflow-auto px-[var(--space-4)] hide-scrollbar contain-strict [-webkit-overflow-scrolling:touch]"
      empty={<HomeInlineEmptyState body={t("home.noAttentionBody")} />}
      estimateSize={() => 144}
      getItemKey={(item) => item.id}
      hasNextPage={query.hasNextPage}
      header={
        // Mirror the projects-tab pill's internal top inset (1px border + p-1 + button py-2) and label
        // typography so the "Inbox" upper edge aligns precisely with the tab labels in the adjacent pane.
        <h2
          className="m-0 mt-[calc(1px+var(--space-1)+var(--space-2))] text-base font-bold"
          id="attention-title"
        >
          {t("home.attentionPane")}
        </h2>
      }
      isFetchingNextPage={query.isFetchingNextPage}
      items={items}
      loadingLabel={t("app.loadingMore")}
      onLoadMore={() => {
        query.fetchNextPage();
      }}
      paddingEnd={16}
      paddingStart={16}
      renderItem={(item) => (
        <AttentionRow item={item} onTaskDetail={onTaskDetail} onSessionChat={onSessionChat} />
      )}
    />
  );
}

function HomeInlineEmptyState({ body }: Readonly<{ body: string }>) {
  return (
    <div className="rounded-[var(--radius-l)] border border-dashed border-[var(--color-outline)] p-[var(--space-4)] text-[var(--color-muted)]">
      <p>{body}</p>
    </div>
  );
}
