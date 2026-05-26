import { Plus } from "lucide-react";
import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import type { ProjectWorkflowLink, WorkflowRecord } from "../../api";
import { errorMessage } from "../../api/errors";
import { queryKeys } from "../../app/queryKeys";
import { useAppServices } from "../../app/useAppServices";
import {
  Button,
  EmptyState,
  ErrorState,
  ItemContent,
  ItemTitle,
  LoadingState,
  VirtualizedInfiniteList,
} from "../../ui";
import { useWorkflowPages } from "./WorkflowData";
import { WorkflowCreateForm } from "./WorkflowCreateForm";

export function LinkWorkflowSidebar({
  onCreated,
  onLinked,
  projectID,
  selectedWorkflowID,
}: Readonly<{
  onCreated: (workflowID: string) => void;
  onLinked: (workflowID: string) => void;
  projectID: string;
  selectedWorkflowID: string;
}>) {
  const { t } = useTranslation();
  const [creating, setCreating] = useState(false);
  if (creating) {
    return (
      <WorkflowCreateForm
        onCreated={(result) => {
          onCreated(result.workflow.id);
        }}
        projectID={projectID}
      />
    );
  }
  return (
    <LinkWorkflowPicker
      onCreate={() => {
        setCreating(true);
      }}
      onLinked={onLinked}
      projectID={projectID}
      selectedWorkflowID={selectedWorkflowID}
      title={t("workflowLibrary.linkWorkflow")}
    />
  );
}

function LinkWorkflowPicker({
  onCreate,
  onLinked,
  projectID,
  selectedWorkflowID,
  title,
}: Readonly<{
  onCreate: () => void;
  onLinked: (workflowID: string) => void;
  projectID: string;
  selectedWorkflowID: string;
  title: string;
}>) {
  const { t } = useTranslation();
  const { api } = useAppServices();
  const queryClient = useQueryClient();
  const workflowsQuery = useWorkflowPages();
  const linksQuery = useQuery({
    queryKey: queryKeys.projectWorkflowLinks(projectID),
    queryFn: async () => api.listProjectWorkflowLinks(projectID),
  });
  const workflows = useMemo(
    () => workflowsQuery.data?.pages.flatMap((page) => page.workflows) ?? [],
    [workflowsQuery.data],
  );
  const linkedByWorkflowID = useMemo(
    () => projectLinksByWorkflowID(linksQuery.data ?? []),
    [linksQuery.data],
  );
  const linkMutation = useMutation({
    mutationFn: async (workflowID: string) => api.linkWorkflowToProject({ projectID, workflowID }),
    onSuccess: async (link) => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.allProjectWorkflowLinks });
      await queryClient.invalidateQueries({ queryKey: queryKeys.allBoards });
      await queryClient.invalidateQueries({ queryKey: queryKeys.allWorkflows });
      onLinked(link.workflowID);
    },
  });

  if (workflowsQuery.isPending || linksQuery.isPending) {
    return <LoadingState appearanceDelayMs={0} fullPage={false} title={title} />;
  }
  if (workflowsQuery.isError) {
    return (
      <ErrorState
        body={errorMessage(workflowsQuery.error)}
        fullPage={false}
        onRetry={() => void workflowsQuery.refetch()}
        retryLabel={t("app.retry")}
        title={t("workflowLibrary.loadFailed")}
      />
    );
  }
  if (linksQuery.isError) {
    return (
      <ErrorState
        body={errorMessage(linksQuery.error)}
        fullPage={false}
        onRetry={() => void linksQuery.refetch()}
        retryLabel={t("app.retry")}
        title={t("workflowEditor.linkLoadFailed")}
      />
    );
  }

  return (
    <div className="grid min-h-0 gap-[var(--space-4)]">
      {linkMutation.isError ? (
        <ErrorState
          body={errorMessage(linkMutation.error)}
          fullPage={false}
          reveal={false}
          title={t("workflowLibrary.linkFailed")}
        />
      ) : null}
      <Button className="justify-self-start" onClick={onCreate} variant="primary">
        <span className="inline-flex items-center gap-[var(--space-2)]">
          <Plus aria-hidden="true" size={16} strokeWidth={1.6} />
          {t("workflowLibrary.newWorkflow")}
        </span>
      </Button>
      <VirtualizedInfiniteList
        className="max-h-[min(560px,60vh)] min-h-[280px] overflow-auto"
        empty={
          <EmptyState
            action={<Button onClick={onCreate}>{t("workflowLibrary.createWorkflow")}</Button>}
            body={t("workflowLibrary.emptyBody")}
            fullPage={false}
            title={t("workflowLibrary.emptyTitle")}
          />
        }
        estimateSize={() => 92}
        getItemKey={(workflow) => workflow.id}
        hasNextPage={workflowsQuery.hasNextPage}
        isFetchingNextPage={workflowsQuery.isFetchingNextPage}
        items={workflows}
        loadingLabel={t("app.loadingMore")}
        onLoadMore={() => void workflowsQuery.fetchNextPage()}
        renderItem={(workflow) => (
          <WorkflowLinkRow
            linked={linkedByWorkflowID.get(workflow.id)}
            linking={linkMutation.isPending}
            onLink={() => void linkMutation.mutateAsync(workflow.id)}
            selected={workflow.id === selectedWorkflowID}
            workflow={workflow}
          />
        )}
      />
    </div>
  );
}

function WorkflowLinkRow({
  linked,
  linking,
  onLink,
  selected,
  workflow,
}: Readonly<{
  linked: ProjectWorkflowLink | undefined;
  linking: boolean;
  onLink: () => void;
  selected: boolean;
  workflow: WorkflowRecord;
}>) {
  const { t } = useTranslation();
  return (
    <div className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-[var(--space-2)] rounded-md border border-[var(--color-outline)] bg-[var(--color-island-1)] px-[var(--space-3)] py-[var(--space-3)]">
      <ItemContent>
        <ItemTitle>{workflow.name}</ItemTitle>
        <span className="text-sm text-[var(--color-muted)]">
          {linked?.isDefault === true
            ? t("workflowLibrary.defaultLinked")
            : linked !== undefined
              ? t("workflowLibrary.linked")
              : selected
                ? t("workflowLibrary.selected")
                : t("workflowLibrary.reusableDefinition")}
        </span>
      </ItemContent>
      <Button disabled={linking} onClick={onLink} variant={linked === undefined ? "primary" : "secondary"}>
        {linked === undefined ? t("workflowLibrary.link") : t("workflowLibrary.select")}
      </Button>
    </div>
  );
}

function projectLinksByWorkflowID(
  links: readonly ProjectWorkflowLink[],
): ReadonlyMap<string, ProjectWorkflowLink> {
  return new Map(links.map((link) => [link.workflowID, link]));
}
