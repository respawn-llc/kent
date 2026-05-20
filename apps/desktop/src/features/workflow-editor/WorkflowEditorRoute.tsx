import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";

import { errorMessage } from "../../api/errors";
import { queryKeys } from "../../app/queryKeys";
import { useWindowChromeTitle } from "../../app/windowChromeTitle";
import { ErrorState, FloatingNoticeIsland, LoadingState } from "../../ui";
import { WorkflowValidationIssues } from "../workflow/WorkflowValidationIssues";
import { WorkflowGraphCanvas } from "./WorkflowGraphCanvas";
import { layoutWorkflowGraph, type WorkflowGraphLayout } from "./workflowGraphLayout";
import { useWorkflowEditorData, type WorkflowEditorData } from "./useWorkflowEditorData";
import type { WorkflowValidation } from "../../api";

export type WorkflowEditorRouteProps = Readonly<{
  projectID: string;
  workflowID: string;
}>;

export function WorkflowEditorRoute({ projectID, workflowID }: WorkflowEditorRouteProps) {
  const { t } = useTranslation();
  const [issuesCollapsed, setIssuesCollapsed] = useState(false);
  const data = useWorkflowEditorData(projectID, workflowID);
  const workflow = data.workflowQuery.data?.workflow;
  const layoutQuery = useWorkflowGraphLayoutQuery(workflowID, data);
  useWindowChromeTitle(workflow === undefined ? t("workflowEditor.title") : workflow.name);

  const viewState = workflowEditorViewState(data, layoutQuery);
  if (viewState.kind === "loading") {
    return <LoadingState appearanceDelayMs={0} title={t("workflowEditor.loadingTitle")} />;
  }
  if (viewState.kind === "linkError") {
    return (
      <ErrorState
        body={errorMessage(viewState.error)}
        onRetry={() => void data.linksQuery.refetch()}
        retryLabel={t("app.retry")}
        title={t("workflowEditor.linkLoadFailed")}
      />
    );
  }
  if (viewState.kind === "unlinked") {
    return (
      <ErrorState
        body={t("workflowEditor.unlinkedBody")}
        reveal={false}
        title={t("workflowEditor.unlinkedTitle")}
      />
    );
  }
  if (viewState.kind === "loadError") {
    return (
      <ErrorState
        body={errorMessage(viewState.error)}
        onRetry={() => {
          void data.boardQuery.refetch();
          void data.workflowQuery.refetch();
          void data.validationQuery.refetch();
          void layoutQuery.refetch();
        }}
        retryLabel={t("app.retry")}
        title={t("workflowEditor.loadFailed")}
      />
    );
  }

  return (
    <section className="h-full min-h-0 w-full" data-testid="workflow-editor-route">
      <WorkflowGraphCanvas graph={viewState.graph} />
      <ValidationIssueIsland
        collapsed={issuesCollapsed}
        onCollapsedChange={setIssuesCollapsed}
        validation={viewState.validation}
      />
    </section>
  );
}

function useWorkflowGraphLayoutQuery(workflowID: string, data: WorkflowEditorData): UseQueryResult<WorkflowGraphLayout> {
  return useQuery({
    queryKey: queryKeys.workflowGraphLayout(
      workflowID,
      data.workflowQuery.data?.workflow.graphRevision ?? 0,
      data.validationQuery.data?.valid ?? false,
      data.validationQuery.data?.errors ?? [],
    ),
    queryFn: async () => {
      const definition = data.workflowQuery.data;
      const validation = data.validationQuery.data;
      if (definition === undefined || validation === undefined) {
        throw new Error("Workflow graph layout requested before workflow definition and validation loaded.");
      }
      return layoutWorkflowGraph(definition, validation);
    },
    enabled: data.workflowQuery.data !== undefined && data.validationQuery.data !== undefined,
  });
}

function ValidationIssueIsland({
  collapsed,
  onCollapsedChange,
  validation,
}: Readonly<{
  collapsed: boolean;
  onCollapsedChange: (collapsed: boolean) => void;
  validation: WorkflowValidation;
}>) {
  const { t } = useTranslation();
  if (validation.valid) {
    return null;
  }
  return (
    <FloatingNoticeIsland
      collapsed={collapsed}
      collapseLabel={t("app.collapse")}
      expandLabel={t("app.expand")}
      onCollapsedChange={onCollapsedChange}
      positionClassName="right-[var(--space-4)] bottom-[var(--space-4)]"
      title={t("board.workflowIssues")}
      tone="danger"
    >
      <WorkflowValidationIssues errors={validation.errors} />
    </FloatingNoticeIsland>
  );
}

type WorkflowEditorViewState =
  | Readonly<{ kind: "loading" }>
  | Readonly<{ error: Error; kind: "linkError" }>
  | Readonly<{ kind: "unlinked" }>
  | Readonly<{ error: Error; kind: "loadError" }>
  | Readonly<{ graph: WorkflowGraphLayout; kind: "ready"; validation: WorkflowValidation }>;

function workflowEditorViewState(
  data: WorkflowEditorData,
  layoutQuery: UseQueryResult<WorkflowGraphLayout>,
): WorkflowEditorViewState {
  if (isLinkGateLoading(data)) {
    return { kind: "loading" };
  }
  if (data.linksQuery.isError) {
    return { error: data.linksQuery.error, kind: "linkError" };
  }
  if (!data.linked) {
    return { kind: "unlinked" };
  }
  if (isGraphLoading(data, layoutQuery)) {
    return { kind: "loading" };
  }
  const loadError = workflowEditorLoadError(data, layoutQuery);
  if (loadError !== null) {
    return { error: loadError, kind: "loadError" };
  }
  if (layoutQuery.data === undefined || data.validationQuery.data === undefined) {
    return { kind: "loading" };
  }
  return { graph: layoutQuery.data, kind: "ready", validation: data.validationQuery.data };
}

function isLinkGateLoading(data: WorkflowEditorData): boolean {
  return data.linksQuery.isPending || data.boardQuery.isPending;
}

function isGraphLoading(data: WorkflowEditorData, layoutQuery: UseQueryResult<WorkflowGraphLayout>): boolean {
  return data.workflowQuery.isPending || data.validationQuery.isPending || layoutQuery.isPending;
}

function workflowEditorLoadError(
  data: WorkflowEditorData,
  layoutQuery: UseQueryResult<WorkflowGraphLayout>,
): Error | null {
  return data.boardQuery.error ?? data.workflowQuery.error ?? data.validationQuery.error ?? layoutQuery.error ?? null;
}
