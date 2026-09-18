import { useMemo } from "react";
import * as Effect from "effect/Effect";
import { useStableCallback } from "@/ui";
import { useTranslation } from "react-i18next";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import { errorMessage, type WorkflowProjectEvent } from "@/api";
import {
  queryKeys,
  useLocalSubscription,
  useProjectObservation,
  ProjectObservationFailure,
} from "@/app-facade";
import { useAppServices } from "@/app-facade";
import { useStatusController } from "@/app-facade";

export type WorkflowEditorData = ReturnType<typeof useWorkflowEditorData>;

export function useWorkflowEditorData(rawProjectID: string, workflowID: string) {
  const { t } = useTranslation();
  const { api } = useAppServices();
  const queryClient = useQueryClient();
  const { push } = useStatusController();
  // A blank or whitespace-only project id is not a real project context (e.g. the
  // editor opened from the global workflow library). Normalizing it here keeps every
  // project-scoped query, subscription, and the link gate off, so the editor never
  // issues a project-scoped RPC with an empty `project_id`.
  const projectID = rawProjectID.trim();
  const linksQuery = useQuery({
    queryKey: queryKeys.projectWorkflowLinks(projectID),
    queryFn: async () => api.listProjectWorkflowLinks(projectID),
    enabled: projectID.length > 0,
  });
  const activeLink = linksQuery.data?.find(
    (link) => link.projectID === projectID && link.workflowID === workflowID,
  );
  const projectContext = projectID.length > 0;
  const linked = !projectContext || activeLink !== undefined;
  const workflowQuery = useQuery({
    queryKey: queryKeys.workflowDefinition(workflowID),
    queryFn: async () => api.getWorkflow(workflowID),
    enabled: linked,
  });
  const validationQuery = useQuery({
    queryKey: queryKeys.workflowValidation(workflowID, "execution"),
    queryFn: async () => api.validateWorkflow(workflowID, "execution"),
    enabled: linked,
  });

  async function refresh(notify: boolean): Promise<void> {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: queryKeys.projectWorkflowLinks(projectID) }),
      queryClient.invalidateQueries({ queryKey: queryKeys.boardWorkflowRoot(projectID, workflowID) }),
      queryClient.invalidateQueries({
        queryKey: queryKeys.boardNodeCardsWorkflowRoot(projectID, workflowID),
      }),
      queryClient.invalidateQueries({ queryKey: queryKeys.workflowDefinition(workflowID) }),
      queryClient.invalidateQueries({ queryKey: queryKeys.workflowValidation(workflowID, "execution") }),
    ]);
    if (notify) {
      push({
        id: "workflow-editor-updated",
        tone: "neutral",
        title: t("workflowEditor.updated"),
      });
    }
  }
  const workflowObservation = useWorkflowEditorSubscription(workflowID, refresh, {
    affectsEditor: (event) => shouldRefreshWorkflowDefinition(event, workflowID),
    shouldNotify: (event) => shouldNotifyWorkflowEditorRefresh(event, projectID, workflowID),
  });
  const projectObservation = useProjectObservation(
    projectContext ? projectID : null,
    projectID,
    (observation) =>
      Effect.tryPromise({
        try: async () => {
          if (observation.kind === "open") await refresh(false);
          if (
            observation.kind === "event" &&
            shouldRefreshWorkflowLink(observation.event, projectID, workflowID)
          ) {
            await refresh(shouldNotifyWorkflowEditorRefresh(observation.event, projectID, workflowID));
          }
        },
        catch: (cause) => new ProjectObservationFailure(cause),
      }),
  );

  return {
    activeLink,
    linked,
    linksQuery,
    projectContext,
    validationQuery,
    workflowQuery,
    workflowObservation,
    projectObservation,
  };
}

function useWorkflowEditorSubscription(
  id: string,
  refresh: (notify: boolean) => Promise<void>,
  {
    affectsEditor,
    shouldNotify,
  }: Readonly<{
    affectsEditor(event: WorkflowProjectEvent): boolean;
    shouldNotify(event: WorkflowProjectEvent): boolean;
  }>,
) {
  const { api } = useAppServices();
  const identity = useMemo(() => ({ id }), [id]);
  const refreshOrFail = useStableCallback((notify: boolean, reportFailure: (error: Error) => void) => {
    void refresh(notify).catch((cause: unknown) => {
      reportFailure(cause instanceof Error ? cause : new Error(errorMessage(cause)));
    });
  });
  const onEvent = useStableCallback((event: WorkflowProjectEvent, reportFailure: (error: Error) => void) => {
    if (affectsEditor(event)) refreshOrFail(shouldNotify(event), reportFailure);
  });
  return useLocalSubscription(id.length > 0 ? identity : null, (reportFailure, isActive) => {
    const handler = {
      onOpen: () => {
        if (isActive()) refreshOrFail(false, reportFailure);
      },
      onEvent: (event: WorkflowProjectEvent) => {
        if (isActive()) onEvent(event, reportFailure);
      },
      onComplete: () => undefined,
      onError: (error: Error) => {
        reportFailure(error);
      },
    };
    return api.subscribeWorkflow(id, handler);
  });
}

export function shouldRefreshWorkflowEditor(
  event: WorkflowProjectEvent,
  projectID: string,
  workflowID: string,
): boolean {
  return (
    shouldRefreshWorkflowDefinition(event, workflowID) ||
    shouldRefreshWorkflowLink(event, projectID, workflowID)
  );
}

export function shouldNotifyWorkflowEditorRefresh(
  event: WorkflowProjectEvent,
  projectID: string,
  workflowID: string,
): boolean {
  if (
    event.resource === "workflow" &&
    event.workflowID === workflowID &&
    workflowDefinitionActions.has(event.action)
  ) {
    return event.action !== "deleted";
  }
  if (
    event.resource === "workflow_link" &&
    event.projectID === projectID &&
    workflowLinkActions.has(event.action) &&
    event.workflowID === workflowID
  ) {
    return event.action !== "unlinked";
  }
  return false;
}

export function shouldRefreshWorkflowDefinition(event: WorkflowProjectEvent, workflowID: string): boolean {
  return (
    event.resource === "workflow" &&
    event.workflowID === workflowID &&
    workflowDefinitionActions.has(event.action)
  );
}

export function shouldRefreshWorkflowLink(
  event: WorkflowProjectEvent,
  projectID: string,
  workflowID: string,
): boolean {
  return (
    event.resource === "workflow_link" &&
    event.projectID === projectID &&
    workflowLinkActions.has(event.action) &&
    event.workflowID === workflowID
  );
}

const workflowDefinitionActions = new Set(["updated", "deleted", "graph_saved"]);

const workflowLinkActions = new Set(["linked", "default_changed", "unlinked"]);
