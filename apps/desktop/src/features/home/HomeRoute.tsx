import { useCallback, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import type { AttentionItem } from "../../api";
import { errorMessage } from "../../api/errors";
import { basename, formatRelativeTime, projectKeyFromName } from "../../app/formatters";
import { useAppNavigation } from "../../app/navigation";
import { queryKeys } from "../../app/queryKeys";
import { useSidebar } from "../../app/sidebarContext";
import { useAppServices } from "../../app/useAppServices";
import { useNativeDialogFallback } from "../../app/useNativeDialogFallback";
import { useStatusController } from "../../app/useStatusController";
import { useConnectionSnapshot } from "../../app/useConnectionSnapshot";
import { ErrorState, LoadingState, VirtualizedInfiniteList } from "../../ui";
import { HomePrimaryPane, type HomePrimaryTab } from "./HomePrimaryPane";
import { ProjectCreateDialog, type ProjectDraft } from "./ProjectCreateForm";
import {
  useGlobalAttentionPages,
  useProjectCreation,
  useProjectCreationEvents,
  useProjectPages,
} from "./useHomeData";

const LOCAL_UNBOUND_PLAN_KIND = "local_unbound";
const attentionPaneItemMaxWidthClassName = "[&>*]:max-w-[600px]";

export function HomeRoute() {
  const { t } = useTranslation();
  const { api, nativeBridge } = useAppServices();
  const { push } = useStatusController();
  const connection = useConnectionSnapshot();
  const navigation = useAppNavigation();
  const { openSidebar } = useSidebar();
  const queryClient = useQueryClient();
  const creation = useProjectCreation();
  const projects = useProjectPages();
  const attention = useGlobalAttentionPages();
  const [primaryTab, setPrimaryTab] = useState<HomePrimaryTab>("projects");
  const projectItems = projects.data?.pages.flatMap((page) => page.projects) ?? [];
  const attentionItems = attention.data?.pages.flatMap((page) => page.items) ?? [];
  const disabled = connection.phase !== "connected";
  const projectCreationDialog = useNativeDialogFallback<ProjectDraft>({
    errorNoticeID: "project-create-window-error",
    errorTitle: t("home.projectCreateWindowError"),
    nativeAvailable: nativeBridge.capabilities.projectCreationWindow,
    openNative: async (nextDraft) => {
      await nativeBridge.projectCreation.openWindow(nextDraft);
    },
    renderFallback: (nextDraft, close) => (
      <ProjectCreateDialog
        creationError={creation.error}
        draft={nextDraft}
        isCreating={creation.isPending}
        onClose={close}
        onSubmitDraft={(values) => void submitDraft(values, close)}
      />
    ),
  });

  async function chooseWorkspace(): Promise<void> {
    try {
      const selected = await nativeBridge.directories.selectDirectory({ title: t("home.chooseWorkspace") });
      if (selected === null) {
        return;
      }
      await openProjectCreationDestination(selected.path);
    } catch (error) {
      push({
        id: "project-create-picker-error",
        tone: "danger",
        title: t("home.workspacePickerError"),
        body: errorMessage(error),
      });
    }
  }

  async function openProjectCreationDestination(workspacePath: string): Promise<void> {
    try {
      const plan = await api.planWorkspace(workspacePath);
      if (plan.binding !== null) {
        void navigation.openProject(plan.binding.projectID);
        return;
      }
      if (plan.kind !== LOCAL_UNBOUND_PLAN_KIND) {
        push({
          id: "project-create-selection-required",
          tone: "info",
          title: t("home.workspaceSelectionRequired"),
          body: t("home.workspaceSelectionRequiredBody"),
        });
        return;
      }
      const name = basename(plan.canonicalRoot);
      const nextDraft = { name, key: projectKeyFromName(name), workspaceRoot: plan.canonicalRoot };
      await projectCreationDialog.open(nextDraft);
    } catch (error) {
      push({
        id: "project-create-plan-error",
        tone: "danger",
        title: t("home.workspacePlanError"),
        body: errorMessage(error),
      });
    }
  }

  async function submitDraft(values: ProjectDraft, close: () => void): Promise<void> {
    try {
      const plan = await api.planWorkspace(values.workspaceRoot);
      if (plan.binding !== null) {
        close();
        void navigation.openProject(plan.binding.projectID);
        return;
      }
      if (plan.kind !== LOCAL_UNBOUND_PLAN_KIND) {
        close();
        push({
          id: "project-create-selection-required",
          tone: "info",
          title: t("home.workspaceSelectionRequired"),
          body: t("home.workspaceSelectionRequiredBody"),
        });
        return;
      }
      const binding = await creation.mutateAsync({
        name: values.name.trim(),
        key: values.key.trim().toUpperCase(),
        workspaceRoot: values.workspaceRoot,
      });
      close();
      void navigation.openProject(binding.projectID);
    } catch (error) {
      push({
        id: "project-create-submit-error",
        tone: "danger",
        title: t("home.workspacePlanError"),
        body: errorMessage(error),
      });
    }
  }

  const handleNativeProjectCreated = useCallback(
    (binding: Readonly<{ projectID: string }>) => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.projects });
      void navigation.openProject(binding.projectID);
    },
    [navigation, queryClient],
  );

  useProjectCreationEvents(handleNativeProjectCreated);

  return (
    <div className="h-full min-h-0" data-testid="home-route-root">
      {projectCreationDialog.fallback}
      <div
        className="grid h-full min-h-0 grid-cols-[repeat(auto-fit,minmax(min(100%,360px),1fr))] gap-[var(--space-2)]"
        data-testid="home-pane-grid"
      >
        <section
          aria-labelledby="home-primary-pane-title"
          className="island-glass min-h-0 overflow-hidden rounded-[var(--radius-xl)]"
        >
          <HomePrimaryPane
            activeTab={primaryTab}
            disabled={disabled}
            onChooseWorkspace={() => void chooseWorkspace()}
            onCreateWorkflow={() => {
              void openSidebar({ kind: "workflowCreate", mode: "overlay" });
            }}
            onTabChange={setPrimaryTab}
            projectItems={projectItems}
            projectsQuery={projects}
          />
        </section>
        <section
          aria-labelledby="attention-title"
          className="island-glass min-h-0 overflow-hidden rounded-[var(--radius-xl)]"
        >
          <AttentionList items={attentionItems} query={attention} />
        </section>
      </div>
    </div>
  );
}

type AttentionListProps = Readonly<{
  items: readonly AttentionItem[];
  query: ReturnType<typeof useGlobalAttentionPages>;
}>;

function AttentionList({ items, query }: AttentionListProps) {
  const { t } = useTranslation();
  const navigation = useAppNavigation();
  const { openSidebar } = useSidebar();
  if (query.isPending) {
    return <LoadingState appearanceDelayMs={0} fullPage={false} reveal={false} title={t("states.loading")} />;
  }
  if (query.isError) {
    return <ErrorState body={errorMessage(query.error)} reveal={false} title={t("states.error")} />;
  }
  return (
    <VirtualizedInfiniteList
      className={`h-full min-h-0 overflow-auto px-[var(--space-4)] hide-scrollbar contain-strict [-webkit-overflow-scrolling:touch] [&>*]:mx-auto [&>*]:w-full ${attentionPaneItemMaxWidthClassName}`}
      empty={<HomeInlineEmptyState body={t("home.noAttentionBody")} />}
      estimateSize={() => 144}
      getItemKey={(item) => item.id}
      hasNextPage={query.hasNextPage}
      header={
        <h2 className="m-0 pb-[var(--space-2)] text-[1.15rem]" id="attention-title">
          {t("home.attentionPane")}
        </h2>
      }
      isFetchingNextPage={query.isFetchingNextPage}
      items={items}
      loadingLabel={t("app.loadingMore")}
      onLoadMore={() => void query.fetchNextPage()}
      paddingEnd={16}
      paddingStart={16}
      renderItem={(item) => <AttentionRow item={item} navigation={navigation} openSidebar={openSidebar} />}
    />
  );
}

function AttentionRow({
  item,
  navigation,
  openSidebar,
}: Readonly<{
  item: AttentionItem;
  navigation: ReturnType<typeof useAppNavigation>;
  openSidebar: ReturnType<typeof useSidebar>["openSidebar"];
}>) {
  return (
    <button
      className="grid w-full min-w-0 gap-[var(--space-2)] rounded-[var(--radius-l)] border border-[var(--color-outline)] bg-[var(--color-island-1)] p-[var(--space-3)] text-left text-[var(--color-on-island)]"
      data-testid="attention-row"
      onClick={() => {
        if (item.taskID.length > 0) {
          void openSidebar({
            kind: "taskDetail",
            initialFocus: item.kind === "question" ? "firstQuestion" : undefined,
            mode: "overlay",
            onMutated: undefined,
            resumeRunID: "",
            taskID: item.taskID,
          });
          return;
        }
        if (item.workflowID.length > 0) {
          void navigation.openWorkflowEditor({
            projectID: item.projectID.length > 0 ? item.projectID : undefined,
            workflowID: item.workflowID,
          });
        }
      }}
      type="button"
    >
      <div
        className="flex min-w-0 flex-wrap items-center gap-[var(--space-2)]"
        data-testid="attention-row-meta"
      >
        {item.taskShortID.length > 0 ? (
          <span className="min-w-0 truncate font-mono text-sm text-[var(--color-muted)]">
            {item.taskShortID}
          </span>
        ) : null}
      </div>
      {item.taskTitle.length > 0 ? <strong className="min-w-0 truncate">{item.taskTitle}</strong> : null}
      <span className="min-w-0 text-sm break-words">{item.message}</span>
      <span className="text-sm text-[var(--color-muted)]">{formatRelativeTime(item.occurredAt)}</span>
    </button>
  );
}

function HomeInlineEmptyState({ body }: Readonly<{ body: string }>) {
  return (
    <div className="rounded-[var(--radius-l)] border border-dashed border-[var(--color-outline)] p-[var(--space-4)] text-[var(--color-muted)]">
      <p>{body}</p>
    </div>
  );
}
