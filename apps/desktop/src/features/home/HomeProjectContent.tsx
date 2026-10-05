import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus } from "lucide-react";
import { desktopChatEnabled } from "@/shared/feature-flags";

import type { SessionCatalogSummary, SessionCategory } from "@/api";
import { errorMessage } from "@/api";
import {
  formatRelativeTime,
  type ProjectContentTab,
  useSessionChatCatalogReturn,
  useAppNavigation,
  readLastProjectRoute,
  writeLastProjectContentTab,
  type SidebarMode,
} from "@/app-facade";
import {
  directionalBoundary,
  EmptyState,
  Item,
  ItemContent,
  ItemTitle,
  InfiniteListBoundary,
  IslandTabs,
  VirtualizedInfiniteList,
} from "@/ui";
import { OverlappingCrossfade } from "./OverlappingCrossfade";
import { ProjectTasksSurface } from "./ProjectTasksSurface";
import { createProjectTasksViewMemory } from "./projectTasksViewMemory";
import { useHomeProjectModel, useHomeSessionPages } from "./HomeProjectModel";

export function HomeProjectContent({
  projectID,
  sessionsVisible,
  sidebarMode,
}: Readonly<{
  projectID: string;
  sessionsVisible: boolean;
  sidebarMode: SidebarMode;
}>) {
  const navigation = useAppNavigation();
  const catalogReturn = useSessionChatCatalogReturn(projectID);
  const [taskListViewMemory] = useState(createProjectTasksViewMemory);
  useEffect(() => {
    catalogReturn?.consume();
  }, [catalogReturn]);
  useHomeProjectModel(projectID, async () => navigation.selectHomeProject(null));
  return sessionsVisible ? (
    <ProjectContentTabs
      catalogReturn={catalogReturn?.category ?? null}
      projectID={projectID}
      sidebarMode={sidebarMode}
      taskListViewMemory={taskListViewMemory}
    />
  ) : (
    <ProjectTasksSurface projectID={projectID} sidebarMode={sidebarMode} viewMemory={taskListViewMemory} />
  );
}

function ProjectContentTabs({
  catalogReturn,
  projectID,
  sidebarMode,
  taskListViewMemory,
}: Readonly<{
  catalogReturn: SessionCategory | null;
  projectID: string;
  sidebarMode: SidebarMode;
  taskListViewMemory: ReturnType<typeof createProjectTasksViewMemory>;
}>) {
  const navigation = useAppNavigation();
  const { t } = useTranslation();
  const [tab, setTab] = useState<ProjectContentTab>(() => {
    if (catalogReturn === "main") return "sessions";
    if (catalogReturn === "subagent") return "subagents";
    const lastProjectRoute = readLastProjectRoute();
    if (lastProjectRoute?.kind === "home_project" && lastProjectRoute.projectId === projectID) {
      return lastProjectRoute.contentTab;
    }
    return "tasks";
  });
  useEffect(() => {
    writeLastProjectContentTab(projectID, tab);
  }, [projectID, tab]);
  const mainSessionsQuery = useHomeSessionPages(projectID, "main", tab === "sessions");
  const subagentSessionsQuery = useHomeSessionPages(projectID, "subagent", tab === "subagents");

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="px-[var(--space-4)] pt-[var(--space-4)]">
        <IslandTabs
          ariaLabel={t("home.prototype.projectContent")}
          className="grid-cols-3"
          items={[
            { label: t("home.prototype.tasks"), value: "tasks" },
            {
              label: t("home.prototype.sessions"),
              value: "sessions",
              action: desktopChatEnabled
                ? {
                    ariaLabel: t("chat.newChat"),
                    children: <Plus size={14} />,
                    onClick: () => {
                      void navigation.openNewChat(projectID);
                    },
                  }
                : undefined,
            },
            { label: t("home.prototype.subagents"), value: "subagents" },
          ]}
          onValueChange={(value) => {
            setTab(value);
          }}
          value={tab}
        />
      </div>
      <div className="min-h-0 flex-1">
        <OverlappingCrossfade contentKey={tab}>
          {tab === "tasks" ? (
            <ProjectTasksSurface
              projectID={projectID}
              sidebarMode={sidebarMode}
              viewMemory={taskListViewMemory}
            />
          ) : (
            <SessionList
              category={tab === "sessions" ? "main" : "subagent"}
              projectID={projectID}
              query={tab === "sessions" ? mainSessionsQuery : subagentSessionsQuery}
            />
          )}
        </OverlappingCrossfade>
      </div>
    </div>
  );
}

function SessionList({
  category,
  projectID,
  query,
}: Readonly<{
  category: "main" | "subagent";
  projectID: string;
  query: Readonly<{
    data:
      | Readonly<{
          pages: readonly Readonly<{ sessions: readonly SessionCatalogSummary[] }>[];
        }>
      | undefined;
    error: Error | null;
    fetchNextPage: () => void;
    hasNextPage: boolean;
    isError: boolean;
    isFetchingNextPage: boolean;
    isPending: boolean;
    refetch: () => void;
  }>;
}>) {
  const { t } = useTranslation();
  const navigation = useAppNavigation();
  const sessions = query.data?.pages.flatMap((page) => page.sessions) ?? [];
  const initialBoundary = directionalBoundary({
    failed: query.isError,
    loading: query.isPending,
    loadingLabel: t("states.loading"),
    message: query.isError ? errorMessage(query.error) : "",
    onRetry: () => {
      query.refetch();
    },
    retryLabel: t("app.retry"),
  });
  return (
    <VirtualizedInfiniteList
      className="h-full min-h-0 overflow-auto px-[var(--space-4)] hide-scrollbar contain-strict"
      empty={
        initialBoundary === undefined ? (
          <EmptyState
            body={t("home.prototype.noSessionsBody")}
            fullPage={false}
            title={t("home.prototype.noSessionsTitle")}
          />
        ) : (
          <InfiniteListBoundary direction="initial" state={initialBoundary} />
        )
      }
      estimateSize={() => 96}
      getItemKey={(session) => session.id}
      hasNextPage={query.hasNextPage}
      isFetchingNextPage={query.isFetchingNextPage}
      items={sessions}
      loadingLabel={t("app.loadingMore")}
      onLoadMore={() => {
        query.fetchNextPage();
      }}
      paddingEnd={16}
      paddingStart={16}
      renderItem={(session) => (
        <Item
          className="min-w-0 px-[var(--space-2)] py-[var(--space-3)]"
          aria-label={session.name ?? session.id}
          onClick={() => {
            void navigation.openSessionChat({
              catalogOrigin: { category },
              projectID,
              sessionID: session.id,
            });
          }}
        >
          <ItemContent className="min-w-0">
            <ItemTitle className="max-w-full">
              <strong className="truncate">{session.name ?? session.id}</strong>
            </ItemTitle>
            {session.firstPromptPreview === null ? null : (
              <span className="line-clamp-2 break-words text-sm text-[var(--color-muted)]">
                {session.firstPromptPreview}
              </span>
            )}
            <span className="text-xs text-[var(--color-muted)]">{formatRelativeTime(session.updatedAt)}</span>
          </ItemContent>
        </Item>
      )}
    />
  );
}
