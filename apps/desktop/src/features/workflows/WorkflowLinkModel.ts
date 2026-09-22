import { MutationObserver, QueryObserver, type QueryClient } from "@tanstack/react-query";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import type { TFunction } from "i18next";
import { errorMessage, isProjectMissingError, type ApiService, type ProjectWorkflowLink } from "@/api";
import { queryAction, queryAtom, queryKeys, type StatusController } from "@/app-facade";

export function createWorkflowLinksModel(api: ApiService, client: QueryClient, projectID: string) {
  const observer = new QueryObserver(client, {
    queryKey: queryKeys.projectWorkflowLinks(projectID),
    queryFn: async () => api.listProjectWorkflowLinks(projectID),
    enabled: projectID.length > 0,
  });
  const request = queryAtom(observer);
  const linkedByWorkflowID = Atom.make(
    (get) => new Map((get(request).data ?? []).map((link) => [link.workflowID, link])),
  );
  const retry = Atom.fn(() => Effect.promise(async () => observer.refetch()), { concurrent: true });
  return { request, linkedByWorkflowID, retry } as const;
}

type LinkSubmission = Readonly<{
  onLinked(workflowID: string): void;
  onProjectMissing?: (() => void) | undefined;
}>;

export function createWorkflowLinkModel({
  api,
  client,
  projectID,
  workflowID,
  push,
  t,
}: Readonly<{
  api: ApiService;
  client: QueryClient;
  projectID: string;
  workflowID: string;
  push: StatusController["push"];
  t: TFunction;
}>) {
  return queryAction(
    new MutationObserver<ProjectWorkflowLink, Error, LinkSubmission>(client, {
      mutationFn: async () => {
        if (projectID.length === 0) throw new Error("Cannot link a workflow without a project.");
        return api.linkWorkflowToProject({ projectID, workflowID });
      },
      onSuccess: async (link, submission) => {
        await client.invalidateQueries({ queryKey: queryKeys.allProjectWorkflowLinks });
        await client.invalidateQueries({ queryKey: queryKeys.allBoards });
        await client.invalidateQueries({ queryKey: queryKeys.allWorkflows });
        submission.onLinked(link.workflowID);
      },
      onError: (error, submission) => {
        push({
          id: `workflow-link-error:${workflowID}`,
          tone: "danger",
          title: t("workflowLibrary.linkFailed"),
          body: errorMessage(error),
        });
        if (isProjectMissingError(error)) submission.onProjectMissing?.();
      },
    }),
  );
}
