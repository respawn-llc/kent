import { useEffect } from "react";
import type { NativeBridge } from "@app/native-bridge";

export type PendingWorkflowGraphDeleteConfirmation = Readonly<{
  requestID: string;
}>;

export function useWorkflowGraphDeleteConfirmationListener<
  TPending extends PendingWorkflowGraphDeleteConfirmation,
>({
  nativeBridge,
  onConfirmed,
  onListenerError,
  pendingDeleteRef,
}: Readonly<{
  nativeBridge: NativeBridge;
  onConfirmed: (deleteRequest: TPending) => void;
  onListenerError?: ((error: unknown) => void) | undefined;
  pendingDeleteRef: { current: TPending | null };
}>): void {
  useEffect(() => {
    let disposed = false;
    let unlisten: (() => void) | null = null;
    void nativeBridge.workflowEditor
      .onGraphDeleteConfirmed((confirmation) => {
        const deleteRequest = pendingDeleteRef.current;
        if (deleteRequest?.requestID !== confirmation.requestID) {
          return;
        }
        onConfirmed(deleteRequest);
      })
      .then((nextUnlisten) => {
        if (disposed) {
          nextUnlisten();
          return;
        }
        unlisten = nextUnlisten;
      })
      .catch((error: unknown) => {
        onListenerError?.(error);
      });
    return () => {
      disposed = true;
      unlisten?.();
    };
  }, [nativeBridge.workflowEditor, onConfirmed, onListenerError, pendingDeleteRef]);
}
