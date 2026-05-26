import { useMemo } from "react";
import { Plus } from "lucide-react";
import { useTranslation } from "react-i18next";

import type { ProjectSummary } from "../../api";
import { errorMessage } from "../../api/errors";
import { useAppNavigation } from "../../app/navigation";
import { EmptyState, ErrorState, LoadingState, VirtualizedInfiniteList } from "../../ui";
import { WorkflowCard } from "../workflows/WorkflowCard";
import { useWorkflowPages } from "../workflows/WorkflowData";
import { ProjectRow } from "./ProjectRow";
import type { useProjectPages } from "./useHomeData";

const homePaneItemMaxWidthClassName = "[&>*]:max-w-[600px]";

export type HomePrimaryTab = "projects" | "workflows";

export type HomePrimaryPaneProps = Readonly<{
  activeTab: HomePrimaryTab;
  disabled: boolean;
  onChooseWorkspace: () => void;
  onCreateWorkflow: () => void;
  onTabChange: (tab: HomePrimaryTab) => void;
  projectItems: readonly ProjectSummary[];
  projectsQuery: ReturnType<typeof useProjectPages>;
}>;

export function HomePrimaryPane({
  activeTab,
  disabled,
  onChooseWorkspace,
  onCreateWorkflow,
  onTabChange,
  projectItems,
  projectsQuery,
}: HomePrimaryPaneProps) {
  const { t } = useTranslation();
  return (
    <div className="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)]">
      <HomePrimaryTabs
        activeTab={activeTab}
        disabled={disabled}
        onChooseWorkspace={onChooseWorkspace}
        onCreateWorkflow={onCreateWorkflow}
        onTabChange={onTabChange}
      />
      <div className="min-h-0" key={activeTab}>
        <h2 className="sr-only" id="home-primary-pane-title">
          {activeTab === "projects" ? t("home.projectsPane") : t("workflowLibrary.homeIslandTitle")}
        </h2>
        <div className="route-transition-frame h-full min-h-0">
          {activeTab === "projects" ? (
            <ProjectList items={projectItems} query={projectsQuery} />
          ) : (
            <HomeWorkflowList />
          )}
        </div>
      </div>
    </div>
  );
}

function HomePrimaryTabs({
  activeTab,
  disabled,
  onChooseWorkspace,
  onCreateWorkflow,
  onTabChange,
}: Readonly<{
  activeTab: HomePrimaryTab;
  disabled: boolean;
  onChooseWorkspace: () => void;
  onCreateWorkflow: () => void;
  onTabChange: (tab: HomePrimaryTab) => void;
}>) {
  const { t } = useTranslation();
  return (
    <div
      aria-label={t("home.projectsPane")}
      className="grid grid-cols-2 gap-[var(--space-2)] px-[var(--space-4)] pt-[var(--space-4)] pb-[var(--space-2)]"
      role="tablist"
    >
      <HomePrimaryTabButton
        active={activeTab === "projects"}
        createLabel={t("home.newProject")}
        disabled={disabled}
        label={t("home.projectsPane")}
        onCreate={onChooseWorkspace}
        onSelect={() => {
          onTabChange("projects");
        }}
      />
      <HomePrimaryTabButton
        active={activeTab === "workflows"}
        createLabel={t("workflowLibrary.createWorkflow")}
        disabled={disabled}
        label={t("workflowLibrary.homeIslandTitle")}
        onCreate={onCreateWorkflow}
        onSelect={() => {
          onTabChange("workflows");
        }}
      />
    </div>
  );
}

function HomePrimaryTabButton({
  active,
  createLabel,
  disabled = false,
  label,
  onCreate,
  onSelect,
}: Readonly<{
  active: boolean;
  createLabel: string;
  disabled?: boolean;
  label: string;
  onCreate: () => void;
  onSelect: () => void;
}>) {
  return (
    <div className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-[var(--space-1)] rounded-full border border-[var(--color-outline)] bg-[var(--color-island-1)] p-1">
      <button
        aria-selected={active}
        className="min-w-0 rounded-full px-[var(--space-3)] py-[var(--space-2)] text-left font-bold text-[var(--color-on-island)] transition-colors data-[active=true]:bg-[var(--color-island-2)]"
        data-active={active}
        onClick={onSelect}
        role="tab"
        type="button"
      >
        <span className="block truncate">{label}</span>
      </button>
      <button
        aria-label={createLabel}
        className="grid h-8 w-8 place-items-center rounded-full border border-[var(--color-outline)] bg-[var(--color-island-1)] text-[var(--color-on-island)] disabled:cursor-not-allowed disabled:opacity-55"
        disabled={disabled}
        onClick={onCreate}
        type="button"
      >
        <Plus aria-hidden="true" size={18} strokeWidth={1.5} />
      </button>
    </div>
  );
}

function ProjectList({
  items,
  query,
}: Readonly<{ items: readonly ProjectSummary[]; query: ReturnType<typeof useProjectPages> }>) {
  const { t } = useTranslation();
  if (query.isPending) {
    return <LoadingState appearanceDelayMs={0} fullPage={false} reveal={false} title={t("states.loading")} />;
  }
  if (query.isError) {
    return <ErrorState body={errorMessage(query.error)} reveal={false} title={t("states.error")} />;
  }
  return (
    <VirtualizedInfiniteList
      className={`h-full min-h-0 overflow-auto px-[var(--space-4)] hide-scrollbar contain-strict [-webkit-overflow-scrolling:touch] [&>*]:mx-auto [&>*]:w-full ${homePaneItemMaxWidthClassName}`}
      empty={<HomeInlineEmptyState body={t("home.emptyBody")} />}
      estimateSize={() => 96}
      getItemKey={(project) => project.id}
      hasNextPage={query.hasNextPage}
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

function HomeWorkflowList() {
  const { t } = useTranslation();
  const navigation = useAppNavigation();
  const workflowsQuery = useWorkflowPages();
  const workflows = useMemo(
    () => workflowsQuery.data?.pages.flatMap((page) => page.workflows) ?? [],
    [workflowsQuery.data],
  );
  if (workflowsQuery.isPending) {
    return <LoadingState appearanceDelayMs={0} fullPage={false} reveal={false} title={t("states.loading")} />;
  }
  if (workflowsQuery.isError) {
    return (
      <ErrorState
        body={errorMessage(workflowsQuery.error)}
        reveal={false}
        title={t("workflowLibrary.loadFailed")}
      />
    );
  }
  return (
    <VirtualizedInfiniteList
      className={`h-full min-h-0 overflow-auto px-[var(--space-4)] hide-scrollbar contain-strict [-webkit-overflow-scrolling:touch] [&>*]:mx-auto [&>*]:w-full ${homePaneItemMaxWidthClassName}`}
      empty={
        <EmptyState
          body={t("workflowLibrary.emptyBody")}
          fullPage={false}
          title={t("workflowLibrary.emptyTitle")}
        />
      }
      estimateSize={() => 96}
      getItemKey={(workflow) => workflow.id}
      hasNextPage={workflowsQuery.hasNextPage}
      isFetchingNextPage={workflowsQuery.isFetchingNextPage}
      items={workflows}
      loadingLabel={t("app.loadingMore")}
      onLoadMore={() => void workflowsQuery.fetchNextPage()}
      paddingEnd={16}
      paddingStart={16}
      renderItem={(workflow) => (
        <WorkflowCard
          onOpen={() => {
            void navigation.openWorkflowEditor({ workflowID: workflow.id });
          }}
          workflow={workflow}
        />
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
