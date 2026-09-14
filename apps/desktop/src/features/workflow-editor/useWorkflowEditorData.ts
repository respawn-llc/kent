import { useEffect, useEffectEvent, useReducer, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import { errorMessage, type WorkflowProjectEvent } from "@/api";
import { queryKeys } from "@/app-facade";
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
  const workflowObservation = useWorkflowEditorSubscription("workflow", workflowID, refresh, {
    affectsEditor: (event) => shouldRefreshWorkflowDefinition(event, workflowID),
    shouldNotify: (event) => shouldNotifyWorkflowEditorRefresh(event, projectID, workflowID),
  });
  const projectObservation = useWorkflowEditorSubscription("project", projectID, refresh, {
    affectsEditor: (event) => shouldRefreshWorkflowLink(event, projectID, workflowID),
    shouldNotify: (event) => shouldNotifyWorkflowEditorRefresh(event, projectID, workflowID),
  });

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
  scope: "workflow" | "project",
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
  const [failure, setFailure] = useState<Readonly<{ id: string; error: Error }> | null>(null);
  const [attempt, retry] = useReducer((value: number) => value + 1, 0);
  const refreshOrFail = useEffectEvent((notify: boolean) => {
    void refresh(notify).catch((cause: unknown) => {
      setFailure({ id, error: cause instanceof Error ? cause : new Error(errorMessage(cause)) });
    });
  });
  const onEvent = useEffectEvent((event: WorkflowProjectEvent) => {
    if (affectsEditor(event)) refreshOrFail(shouldNotify(event));
  });
  useEffect(() => {
    if (id.length === 0) return;
    const handler = {
      onOpen: () => {
        refreshOrFail(false);
      },
      onEvent: (event: WorkflowProjectEvent) => {
        onEvent(event);
      },
      onComplete: () => undefined,
      onError: (error: Error) => {
        setFailure({ id, error });
      },
    };
    const subscription =
      scope === "workflow" ? api.subscribeWorkflow(id, handler) : api.subscribeProject(id, handler);
    return () => {
      subscription.close();
    };
  }, [api, attempt, id, scope]);
  return {
    error: failure?.id === id ? failure.error : null,
    retry: () => {
      setFailure(null);
      retry();
    },
  };
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
