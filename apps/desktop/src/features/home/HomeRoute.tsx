import { useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Plus } from "lucide-react";

import type { AttentionItem, ProjectSummary } from "../../api";
import { errorMessage } from "../../api/errors";
import { basename, formatRelativeTime, projectKeyFromName } from "../../app/formatters";
import { useAppNavigation } from "../../app/navigation";
import { queryKeys } from "../../app/queryKeys";
import { useAppServices } from "../../app/useAppServices";
import { useNativeDialogFallback } from "../../app/useNativeDialogFallback";
import { useStatusController } from "../../app/useStatusController";
import { useConnectionSnapshot } from "../../app/useConnectionSnapshot";
import { Badge, ErrorState, VirtualizedInfiniteList } from "../../ui";
import { useOpenTaskDetail } from "../task-detail/useOpenTaskDetail";
import { ProjectCreateDialog, type ProjectDraft } from "./ProjectCreateForm";
import { ProjectRow } from "./ProjectRow";
import {
  useGlobalAttentionPages,
  useProjectCreation,
  useProjectCreationEvents,
  useProjectPages,
} from "./useHomeData";

const LOCAL_UNBOUND_PLAN_KIND = "local_unbound";

export function HomeRoute() {
  const { t } = useTranslation();
  const { api, nativeBridge } = useAppServices();
  const { push } = useStatusController();
  const connection = useConnectionSnapshot();
  const navigation = useAppNavigation();
  const queryClient = useQueryClient();
  const creation = useProjectCreation();
  const projects = useProjectPages();
  const attention = useGlobalAttentionPages();
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
          aria-labelledby="projects-title"
          className="island-glass min-h-0 overflow-hidden rounded-[var(--radius-xl)]"
        >
          <ProjectList
            disabled={disabled}
            items={projectItems}
            onChooseWorkspace={() => void chooseWorkspace()}
            query={projects}
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

type ProjectListProps = Readonly<{
  disabled: boolean;
  items: readonly ProjectSummary[];
  onChooseWorkspace: () => void;
  query: ReturnType<typeof useProjectPages>;
}>;

function ProjectList({ disabled, items, onChooseWorkspace, query }: ProjectListProps) {
  const { t } = useTranslation();
  if (query.isPending) {
    return <p>{t("states.loading")}</p>;
  }
  if (query.isError) {
    return <ErrorState body={errorMessage(query.error)} reveal={false} title={t("states.error")} />;
  }
  return (
    <VirtualizedInfiniteList
      className="h-full min-h-0 overflow-auto px-[var(--space-4)] hide-scrollbar contain-strict [-webkit-overflow-scrolling:touch] [&>*]:mx-auto [&>*]:w-full [&>*]:max-w-[var(--content-max-width-home-pane)]"
      empty={<HomeInlineEmptyState body={t("home.emptyBody")} />}
      estimateSize={() => 96}
      getItemKey={(project) => project.id}
      hasNextPage={query.hasNextPage}
      header={<ProjectListHeader disabled={disabled} onChooseWorkspace={onChooseWorkspace} />}
      isFetchingNextPage={query.isFetchingNextPage}
      items={items}
      loadingLabel={t("app.loadingMore")}
      onLoadMore={() => void query.fetchNextPage()}
      paddingEnd={16}
      paddingStart={16}
      renderItem={(project) => <ProjectRow project={project} />}
    />
  );
}

function ProjectListHeader({
  disabled,
  onChooseWorkspace,
}: Readonly<{ disabled: boolean; onChooseWorkspace: () => void }>) {
  const { t } = useTranslation();
  return (
    <div className="flex items-center justify-between gap-[var(--space-3)] pb-[var(--space-2)]">
      <h2 className="m-0 text-[1.15rem]" id="projects-title">
        {t("home.projectsPane")}
      </h2>
      <button
        aria-label={t("home.newProject")}
        className="grid h-9 w-9 place-items-center rounded-full border border-[var(--color-outline)] bg-[var(--color-island-1)] text-[var(--color-on-island)] disabled:cursor-not-allowed disabled:opacity-55"
        disabled={disabled}
        onClick={onChooseWorkspace}
        type="button"
      >
        <Plus aria-hidden="true" size={20} strokeWidth={1.5} />
      </button>
    </div>
  );
}

type AttentionListProps = Readonly<{
  items: readonly AttentionItem[];
  query: ReturnType<typeof useGlobalAttentionPages>;
}>;

function AttentionList({ items, query }: AttentionListProps) {
  const { t } = useTranslation();
  const openTaskDetail = useOpenTaskDetail();
  if (query.isPending) {
    return <p>{t("states.loading")}</p>;
  }
  if (query.isError) {
    return <ErrorState body={errorMessage(query.error)} reveal={false} title={t("states.error")} />;
  }
  return (
    <VirtualizedInfiniteList
      className="h-full min-h-0 overflow-auto px-[var(--space-4)] hide-scrollbar contain-strict [-webkit-overflow-scrolling:touch] [&>*]:mx-auto [&>*]:w-full [&>*]:max-w-[var(--content-max-width-home-pane)]"
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
      renderItem={(item) => <AttentionRow item={item} openTaskDetail={openTaskDetail} />}
    />
  );
}

function AttentionRow({
  item,
  openTaskDetail,
}: Readonly<{
  item: AttentionItem;
  openTaskDetail: ReturnType<typeof useOpenTaskDetail>;
}>) {
  return (
    <button
      className="grid w-full min-w-0 gap-[var(--space-2)] rounded-[var(--radius-l)] border border-[var(--color-outline)] bg-[var(--color-island-1)] p-[var(--space-3)] text-left text-[var(--color-on-island)]"
      data-testid="attention-row"
      onClick={() => {
        if (item.taskID.length > 0) {
          openTaskDetail(item.taskID);
        }
      }}
      type="button"
    >
      <div
        className="flex min-w-0 flex-wrap items-center gap-[var(--space-2)]"
        data-testid="attention-row-meta"
      >
        <Badge tone="warning">{item.kind}</Badge>
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
