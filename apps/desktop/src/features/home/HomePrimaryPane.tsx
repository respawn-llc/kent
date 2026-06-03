import { useLayoutEffect, useMemo, useRef, useState, type Ref } from "react";
import { Plus } from "lucide-react";
import { useTranslation } from "react-i18next";

import type { ProjectSummary } from "../../api";
import { errorMessage } from "../../api/errors";
import { useAppNavigation } from "../../app/navigation";
import { EmptyState, ErrorState, IslandSurface, LoadingState, VirtualizedInfiniteList } from "../../ui";
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
  const controlsRef = useRef<HTMLDivElement | null>(null);
  const controlsHeight = useElementHeight(controlsRef);
  return (
    <div className="relative h-full min-h-0" data-testid="home-primary-pane">
      <HomePrimaryTabs
        activeTab={activeTab}
        controlsRef={controlsRef}
        disabled={disabled}
        onChooseWorkspace={onChooseWorkspace}
        onCreateWorkflow={onCreateWorkflow}
        onTabChange={onTabChange}
      />
      <div className="h-full min-h-0" key={activeTab}>
        <h2 className="sr-only" id="home-primary-pane-title">
          {activeTab === "projects" ? t("home.projectsPane") : t("workflowLibrary.homeIslandTitle")}
        </h2>
        <div className="route-transition-frame h-full min-h-0" data-testid="home-primary-scroll-layer">
          {activeTab === "projects" ? (
            <ProjectList controlsHeight={controlsHeight} items={projectItems} query={projectsQuery} />
          ) : (
            <HomeWorkflowList controlsHeight={controlsHeight} />
          )}
        </div>
      </div>
    </div>
  );
}

function HomePrimaryTabs({
  activeTab,
  controlsRef,
  disabled,
  onChooseWorkspace,
  onCreateWorkflow,
  onTabChange,
}: Readonly<{
  activeTab: HomePrimaryTab;
  controlsRef: Ref<HTMLDivElement>;
  disabled: boolean;
  onChooseWorkspace: () => void;
  onCreateWorkflow: () => void;
  onTabChange: (tab: HomePrimaryTab) => void;
}>) {
  const { t } = useTranslation();
  return (
    <div
      aria-label={t("home.projectsPane")}
      className="pointer-events-none absolute inset-x-0 top-0 z-10 grid grid-cols-2 gap-[var(--space-2)] px-[var(--space-4)] pt-[var(--space-4)] pb-[var(--space-2)]"
      data-testid="home-primary-controls"
      ref={controlsRef}
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
    <IslandSurface
      className="pointer-events-auto grid grid-cols-[minmax(0,1fr)_auto] items-center gap-[var(--space-1)] rounded-full p-1"
      data-testid="home-primary-tab-island"
      level={1}
    >
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
        className="grid h-8 w-8 place-items-center rounded-full border border-[var(--color-outline)] bg-[var(--color-island-2)] text-[var(--color-on-island)] disabled:cursor-not-allowed disabled:opacity-55"
        disabled={disabled}
        onClick={onCreate}
        type="button"
      >
        <Plus aria-hidden="true" size={18} strokeWidth={1.5} />
      </button>
    </IslandSurface>
  );
}

function ProjectList({
  controlsHeight,
  items,
  query,
}: Readonly<{
  controlsHeight: number;
  items: readonly ProjectSummary[];
  query: ReturnType<typeof useProjectPages>;
}>) {
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
      paddingStart={controlsHeight}
      renderItem={(project) => <ProjectRow project={project} />}
    />
  );
}

function HomeWorkflowList({ controlsHeight }: Readonly<{ controlsHeight: number }>) {
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
      paddingStart={controlsHeight}
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

function useElementHeight(ref: Readonly<{ current: HTMLElement | null }>): number {
  const [height, setHeight] = useState(0);
  useLayoutEffect(() => {
    const element = ref.current;
    if (element === null) {
      return;
    }
    const measure = () => {
      const nextHeight = element.getBoundingClientRect().height;
      setHeight((currentHeight) => (currentHeight === nextHeight ? currentHeight : nextHeight));
    };
    measure();
    if (typeof ResizeObserver === "undefined") {
      return;
    }
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => {
      observer.disconnect();
    };
  }, [ref]);
  return height;
}

function HomeInlineEmptyState({ body }: Readonly<{ body: string }>) {
  return (
    <div className="rounded-[var(--radius-l)] border border-dashed border-[var(--color-outline)] p-[var(--space-4)] text-[var(--color-muted)]">
      <p>{body}</p>
    </div>
  );
}
