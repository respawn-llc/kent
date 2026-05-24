import { Plus } from "lucide-react";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";

import { errorMessage } from "../../api/errors";
import { useAppNavigation } from "../../app/navigation";
import { useSidebar } from "../../app/sidebarContext";
import { Button, EmptyState, ErrorState, LoadingState, VirtualizedInfiniteList } from "../../ui";
import { WorkflowCard } from "./WorkflowCard";
import { useWorkflowPages } from "./WorkflowData";

export function WorkflowLibraryRoute() {
  const { t } = useTranslation();
  const navigation = useAppNavigation();
  const { openSidebar } = useSidebar();
  const workflowsQuery = useWorkflowPages();
  const workflows = useMemo(
    () => workflowsQuery.data?.pages.flatMap((page) => page.workflows) ?? [],
    [workflowsQuery.data],
  );

  if (workflowsQuery.isPending) {
    return <LoadingState appearanceDelayMs={0} title={t("workflowLibrary.title")} />;
  }
  if (workflowsQuery.isError) {
    return (
      <ErrorState
        body={errorMessage(workflowsQuery.error)}
        chromePadding
        onRetry={() => void workflowsQuery.refetch()}
        retryLabel={t("app.retry")}
        title={t("workflowLibrary.loadFailed")}
      />
    );
  }

  return (
    <section className="h-full min-h-0" data-testid="workflow-library-route">
      <div className="island-glass grid h-full min-h-0 overflow-hidden rounded-[var(--radius-xl)]">
        <VirtualizedInfiniteList
          className="h-full min-h-0 overflow-auto px-[var(--space-4)] hide-scrollbar contain-strict [-webkit-overflow-scrolling:touch] [&>*]:mx-auto [&>*]:w-full [&>*]:max-w-[var(--content-max-width-workflow-library)]"
          empty={
            <EmptyState
              action={
                <Button
                  onClick={() => {
                    void openSidebar({ kind: "workflowCreate", mode: "overlay" });
                  }}
                  variant="primary"
                >
                  {t("workflowLibrary.createWorkflow")}
                </Button>
              }
              body={t("workflowLibrary.emptyBody")}
              fullPage={false}
              title={t("workflowLibrary.emptyTitle")}
            />
          }
          estimateSize={() => 96}
          getItemKey={(workflow) => workflow.id}
          hasNextPage={workflowsQuery.hasNextPage}
          header={
            <WorkflowLibraryHeader
              onCreate={() => {
                void openSidebar({ kind: "workflowCreate", mode: "overlay" });
              }}
            />
          }
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
      </div>
    </section>
  );
}

function WorkflowLibraryHeader({ onCreate }: Readonly<{ onCreate: () => void }>) {
  const { t } = useTranslation();
  return (
    <div className="flex items-center justify-between gap-[var(--space-3)] pb-[var(--space-2)]">
      <h1 className="m-0 text-[1.15rem]" id="workflow-library-title">
        {t("workflowLibrary.title")}
      </h1>
      <button
        aria-label={t("workflowLibrary.createWorkflow")}
        className="grid h-9 w-9 place-items-center rounded-full border border-[var(--color-outline)] bg-[var(--color-island-1)] text-[var(--color-on-island)]"
        onClick={onCreate}
        type="button"
      >
        <Plus aria-hidden="true" size={18} strokeWidth={1.6} />
      </button>
    </div>
  );
}
