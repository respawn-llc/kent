import { useMemo, type ReactNode } from "react";

import {
  createWorkflowEditorDraftBridge,
  WorkflowEditorDraftBridgeContext,
} from "./workflowEditorDraftBridgeCore";

export function WorkflowEditorDraftBridgeProvider({ children }: Readonly<{ children: ReactNode }>) {
  const bridge = useMemo(() => createWorkflowEditorDraftBridge(), []);
  return (
    <WorkflowEditorDraftBridgeContext.Provider value={bridge}>
      {children}
    </WorkflowEditorDraftBridgeContext.Provider>
  );
}
