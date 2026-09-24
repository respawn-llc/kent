import { MutationObserver, type QueryClient } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import {
  errorMessage,
  isProjectMissingError,
  type ApiService,
  type ProjectWorkflowLink,
  type WorkflowRecord,
} from "@/api";
import { queryAction, queryKeys, type StatusController } from "@/app-facade";

export type WorkflowCreateResult = Readonly<{
  workflow: WorkflowRecord;
  link: ProjectWorkflowLink | null;
}>;

type Submission = Readonly<{
  name: string;
  description: string;
  onCreated(result: WorkflowCreateResult): void;
  onProjectMissing?: (() => void) | undefined;
}>;

export function createWorkflowCreateModel({
  api,
  client,
  projectID,
  push,
  t,
}: Readonly<{
  api: ApiService;
  client: QueryClient;
  projectID: string | undefined;
  push: StatusController["push"];
  t: TFunction;
}>) {
  return queryAction(
    new MutationObserver(client, {
      mutationFn: async ({ name, description }: Submission): Promise<WorkflowCreateResult> => {
        const input = { name, description };
        if (projectID === undefined) return { workflow: await api.createWorkflow(input), link: null };
        return api.createAndLinkWorkflowToProject({ ...input, projectID });
      },
      onSuccess: async (result, submission) => {
        await client.invalidateQueries({ queryKey: queryKeys.allWorkflows });
        if (projectID !== undefined) {
          await client.invalidateQueries({ queryKey: queryKeys.allProjectWorkflowLinks });
          await client.invalidateQueries({ queryKey: queryKeys.allBoards });
        }
        submission.onCreated(result);
      },
      onError: (error, submission) => {
        push({
          id: "workflow-create-error",
          tone: "danger",
          title: t("workflowLibrary.createFailed"),
          body: errorMessage(error),
        });
        if (isProjectMissingError(error)) submission.onProjectMissing?.();
      },
    }),
  );
}
