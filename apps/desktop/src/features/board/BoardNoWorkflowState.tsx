import { useTranslation } from "react-i18next";

import { useAppNavigation, useOwnedSidebarRoots } from "@/app-facade";
import { Button, EmptyState } from "@/ui";
import { completeBoardWorkflowLink } from "./boardWorkflowLinkCompletion";

export function BoardNoWorkflowState({ projectID }: Readonly<{ projectID: string }>) {
  const { t } = useTranslation();
  const { open } = useOwnedSidebarRoots();
  const navigation = useAppNavigation();
  return (
    <EmptyState
      actions={
        <>
          <Button
            onClick={() => {
              open({
                kind: "linkWorkflow",
                mode: "overlay",
                onCompleted: async (completion) => {
                  await completeBoardWorkflowLink(navigation, projectID, completion);
                },
                projectID,
              });
            }}
          >
            {t("workflowLibrary.linkWorkflow")}
          </Button>
          <Button
            onClick={() => {
              open({ kind: "workflowCreate", mode: "overlay", projectID });
            }}
            variant="primary"
          >
            {t("workflowLibrary.createWorkflow")}
          </Button>
        </>
      }
      body={t("board.noWorkflowBody")}
      chromePadding
      title={t("board.noWorkflowTitle")}
    />
  );
}
