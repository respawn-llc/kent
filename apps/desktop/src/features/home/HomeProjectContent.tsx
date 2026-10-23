import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { PictureInPicture, Plus } from "lucide-react";

import { sessionDisplayTitle, type SessionCatalogSummary, type SessionCategory } from "@/api";
import { errorMessage } from "@/api";
import {
  formatRelativeTime,
  useAppServices,
  useNativeChatOpening,
  type SessionChatTarget,
  type ProjectContentTab,
  useSessionChatCatalogReturn,
  useAppNavigation,
  projectOpeningTab,
  useDesktopSettings,
  writeLastProjectContentTab,
} from "@/app-facade";
import {
  directionalBoundary,
  EmptyState,
  IconTooltipButton,
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
}: Readonly<{
  projectID: string;
}>) {
  const navigation = useAppNavigation();
  const catalogReturn = useSessionChatCatalogReturn(projectID);
  const [taskListViewMemory] = useState(createProjectTasksViewMemory);
  useEffect(() => {
    catalogReturn?.consume();
  }, [catalogReturn]);
  useHomeProjectModel(projectID, async () => navigation.selectHomeProject(null));
  return (
    <ProjectContentTabs
      catalogReturn={catalogReturn?.category ?? null}
      projectID={projectID}

      taskListViewMemory={taskListViewMemory}
    />
  );
}

function ProjectContentTabs({
  catalogReturn,
  projectID,
  taskListViewMemory,
}: Readonly<{
  catalogReturn: SessionCategory | null;
  projectID: string;
  taskListViewMemory: ReturnType<typeof createProjectTasksViewMemory>;
}>) {
  const navigation = useAppNavigation();
  const { t } = useTranslation();
  const { storageNamespace } = useAppServices();
  const settings = useDesktopSettings();
  const [tab, setTab] = useState<ProjectContentTab>(() => {
    return projectOpeningTab(
      storageNamespace,
      projectID,
      settings.saved.projectOpening,
      catalogReturn === "main" ? "sessions" : catalogReturn === "subagent" ? "subagents" : null,
    );
  });
  useEffect(() => {
    writeLastProjectContentTab(storageNamespace, projectID, tab);
  }, [storageNamespace, projectID, tab]);
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
              action: {
                ariaLabel: t("chat.newChat"),
                children: <Plus size={14} />,
                onClick: () => {
                  void navigation.openNewChat(projectID);
                },
              },
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
  const { nativeBridge } = useAppServices();
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
        <SessionCatalogRow
          category={category}
          nativeWindowsAvailable={nativeBridge.capabilities.dialogWindows}
          onOpen={async (target) => navigation.openSessionChat(target)}
          projectID={projectID}
          session={session}
        />
      )}
    />
  );
}

function SessionCatalogRow({
  category,
  nativeWindowsAvailable,
  onOpen,
  projectID,
  session,
}: Readonly<{
  category: "main" | "subagent";
  nativeWindowsAvailable: boolean;
  onOpen(target: SessionChatTarget): void;
  projectID: string;
  session: SessionCatalogSummary;
}>) {
  const { t } = useTranslation();
  const nativeChat = useNativeChatOpening();
  const title = sessionDisplayTitle(session, (id) => t("home.subagentTitle", { id }));
  const pending = nativeChat.pendingSessionID === session.id;
  const target = { projectID, sessionID: session.id };
  return (
    <div className="group/session-catalog relative min-w-0">
      <Item
        className="absolute inset-0 h-full group-hover/session-catalog:bg-[var(--color-island-1)]"
        aria-label={title}
        onClick={(event) => {
          if (nativeWindowsAvailable && event.shiftKey) {
            if (pending) return;
            nativeChat.open(target);
            return;
          }
          onOpen({ ...target, catalogOrigin: { category } });
        }}
      />
      <ItemContent className="pointer-events-none relative min-w-0 px-[var(--space-2)] py-[var(--space-3)]">
        <div className="flex min-w-0 items-center gap-[var(--space-2)]">
          <ItemTitle className="min-w-0 flex-1">
            <strong className="min-w-0 flex-1 truncate">{title}</strong>
          </ItemTitle>
          {nativeWindowsAvailable ? (
            <span className="pointer-events-auto shrink-0">
              <IconTooltipButton
                label={t("app.popOut")}
                tooltip={t("app.popOut")}
                loading={pending}
                disabled={pending}
                onClick={() => {
                  nativeChat.open(target);
                }}
              >
                <PictureInPicture aria-hidden="true" size={18} strokeWidth={1.5} />
              </IconTooltipButton>
            </span>
          ) : null}
        </div>
        {session.firstPromptPreview === null ? null : (
          <span className="min-w-0 w-full line-clamp-2 break-words text-sm text-[var(--color-muted)]">
            {session.firstPromptPreview}
          </span>
        )}
        <span className="text-xs text-[var(--color-muted)]">{formatRelativeTime(session.updatedAt)}</span>
      </ItemContent>
    </div>
  );
}
