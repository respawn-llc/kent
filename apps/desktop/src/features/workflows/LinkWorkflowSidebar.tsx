import { useMemo, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { useTranslation } from "react-i18next";

import type { ProjectWorkflowLink, WorkflowRecord } from "@/api";
import { errorMessage, isProjectMissingError } from "@/api";
import { useQueryAction, useStatusController } from "@/app-facade";
import type { SidebarPageNavigator } from "@/app-facade";
import { useAppServices } from "@/app-facade";
import { useSidebarBackWhen } from "@/app-facade";
import { WorkflowActionsContextMenu, useWorkflowPages } from "@/shared/workflow-library";
import {
  Button,
  EmptyState,
  ErrorState,
  ItemContent,
  ItemTitle,
  LoadingState,
  Spinner,
  VirtualizedInfiniteList,
} from "@/ui";
import { WorkflowCreateForm } from "./WorkflowCreateForm";
import { createWorkflowLinkModel, createWorkflowLinksModel } from "./WorkflowLinkModel";

export function LinkWorkflowSidebar({
  creating,
  onCreated,
  onLinked,
  projectID,
  navigator,
  selectedWorkflowID,
}: Readonly<{
  creating: boolean;
  onCreated: (workflowID: string) => void;
  onLinked: (workflowID: string) => void;
  projectID: string;
  navigator?: SidebarPageNavigator | undefined;
  selectedWorkflowID?: string | undefined;
}>) {
  const { t } = useTranslation();
  if (creating) {
    return (
      <WorkflowCreateForm
        onCreated={(result) => {
          onCreated(result.workflow.id);
        }}
        onProjectMissing={navigator?.back}
        projectID={projectID}
      />
    );
  }
  return (
    <LinkWorkflowPicker
      onLinked={onLinked}
      projectID={projectID}
      navigator={navigator}
      selectedWorkflowID={selectedWorkflowID}
      title={t("workflowLibrary.linkWorkflow")}
    />
  );
}

function LinkWorkflowPicker({
  onLinked,
  projectID,
  navigator,
  selectedWorkflowID,
  title,
}: Readonly<{
  onLinked: (workflowID: string) => void;
  projectID: string;
  navigator?: SidebarPageNavigator | undefined;
  selectedWorkflowID?: string | undefined;
  title: string;
}>) {
  const { t } = useTranslation();
  const { api } = useAppServices();
  const queryClient = useQueryClient();
  const normalizedProjectID = projectID.trim();
  const workflowsQuery = useWorkflowPages();
  const [model] = useState(() => createWorkflowLinksModel(api, queryClient, normalizedProjectID));
  const linksQuery = useAtomValue(model.request);
  const retryLinks = useAtomSet(model.retry);
  const workflows = useMemo(
    () => workflowsQuery.data?.pages.flatMap((page) => page.workflows) ?? [],
    [workflowsQuery.data],
  );
  const linkedByWorkflowID = useAtomValue(model.linkedByWorkflowID);
  const projectMissing = isProjectMissingError(linksQuery.error);
  useSidebarBackWhen(projectMissing, navigator);

  if (workflowsQuery.isPending || linksQuery.isPending) {
    return <LoadingState appearanceDelayMs={0} fullPage={false} title={title} />;
  }
  if (workflowsQuery.isError) {
    return (
      <ErrorState
        body={errorMessage(workflowsQuery.error)}
        fullPage={false}
        onRetry={() => {
          workflowsQuery.refetch();
        }}
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
        onRetry={() => {
          retryLinks();
        }}
        retryLabel={t("app.retry")}
        title={t("workflowEditor.linkLoadFailed")}
      />
    );
  }

  const list = (
    <VirtualizedInfiniteList
      className="h-full min-h-0 overflow-auto"
      empty={
        <EmptyState
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
      onLoadMore={() => {
        workflowsQuery.fetchNextPage();
      }}
      renderItem={(workflow) => (
        <WorkflowLinkRow
          linked={linkedByWorkflowID.get(workflow.id)}
          onLinked={onLinked}
          projectID={normalizedProjectID}
          navigator={navigator}
          selected={workflow.id === selectedWorkflowID}
          workflow={workflow}
        />
      )}
    />
  );
  return <div className="h-full min-h-0">{list}</div>;
}

function WorkflowLinkRow({
  linked,
  onLinked,
  projectID,
  navigator,
  selected,
  workflow,
}: Readonly<{
  linked: ProjectWorkflowLink | undefined;
  onLinked: (workflowID: string) => void;
  projectID: string;
  navigator?: SidebarPageNavigator | undefined;
  selected: boolean;
  workflow: WorkflowRecord;
}>) {
  const { t } = useTranslation();
  const { api } = useAppServices();
  const client = useQueryClient();
  const { push } = useStatusController();
  const [model] = useState(() =>
    createWorkflowLinkModel({ api, client, projectID, workflowID: workflow.id, push, t }),
  );
  const link = useQueryAction(model);
  const row = (loading: boolean) => (
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
      <div className="flex items-center gap-[var(--space-2)]">
        {loading || link.isPending ? <Spinner size="sm" /> : null}
        <Button
          disabled={link.isPending}
          onClick={() => {
            link.submit({ onLinked, onProjectMissing: navigator?.back });
          }}
          variant={linked === undefined ? "primary" : "secondary"}
        >
          {linked === undefined ? t("workflowLibrary.link") : t("workflowLibrary.select")}
        </Button>
      </div>
    </div>
  );
  return (
    <WorkflowActionsContextMenu
      onEdit={() => {
        navigator?.replace({ kind: "workflowEditor", mode: "overlay", projectID, workflowID: workflow.id });
      }}
      workflowID={workflow.id}
    >
      {row}
    </WorkflowActionsContextMenu>
  );
}
