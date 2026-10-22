import { useTranslation } from "react-i18next";
import { useAppServices, usePublishSidebarHeaderAction, type WorkflowInspectorSelection } from "@/app-facade";
import { writeClipboardText } from "@/shared/native-clipboard";
import { CopyableValueButton, showStatusToast } from "@/ui";
import { WorkflowDeleteButton } from "./WorkflowDeleteButton";

export function WorkflowInspectorHeader({
  selection,
  workflowID,
  onDeleted,
}: Readonly<{
  selection: WorkflowInspectorSelection;
  workflowID: string;
  onDeleted: () => void;
}>) {
  usePublishSidebarHeaderAction(
    selection.kind === "workflow" ? (
      <WorkflowDeleteButton onDeleted={onDeleted} workflowID={workflowID} />
    ) : selection.kind === "node" ? (
      <WorkflowEntityIDHeader entityID={selection.nodeID} entityKind="node" />
    ) : selection.kind === "edge" ? (
      <WorkflowEntityIDHeader entityID={selection.edgeID} entityKind="edge" />
    ) : null,
  );
  return null;
}

function WorkflowEntityIDHeader({
  entityID,
  entityKind,
}: Readonly<{
  entityID: string;
  entityKind: "edge" | "node";
}>) {
  const { t } = useTranslation();
  const { nativeBridge } = useAppServices();
  const node = entityKind === "node";
  return (
    <CopyableValueButton
      accessibleLabel={
        node
          ? t("workflowEditor.copyNodeId", { id: entityID })
          : t("workflowEditor.copyEdgeId", { id: entityID })
      }
      className="max-w-full justify-self-end overflow-hidden text-ellipsis whitespace-nowrap font-mono text-xs"
      onActivate={() => {
        void writeClipboardText(entityID, nativeBridge)
          .then(() => {
            showStatusToast({
              id: `workflow-${entityKind}-id-copied-${entityID}`,
              title: node ? t("workflowEditor.nodeIdCopied") : t("workflowEditor.edgeIdCopied"),
              tone: "success",
            });
          })
          .catch(() => {
            showStatusToast({
              id: `workflow-${entityKind}-id-copy-failed-${entityID}`,
              title: node ? t("workflowEditor.nodeIdCopyFailed") : t("workflowEditor.edgeIdCopyFailed"),
              tone: "danger",
            });
          });
      }}
    >
      {entityID}
    </CopyableValueButton>
  );
}
