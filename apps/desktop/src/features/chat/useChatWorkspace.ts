import { useEffect, useRef, useState } from "react";
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import type { WorkspaceCatalogRow } from "@/api";
import { queryKeys, useAppServices, workspaceCatalogInfiniteQueryOptions } from "@/app-facade";
import { updateWorkspaceSelection } from "@/shared/workspaces";
import type { ChatDestinationOpening } from "./ChatDestinationViewModel";

export function useChatWorkspace(
  selection: ChatDestinationOpening,
  selectWorkspace: (workspace: WorkspaceCatalogRow) => void,
) {
  const services = useAppServices();
  const client = useQueryClient();
  const [workspaceOpen, setWorkspaceOpen] = useState(false);
  const unresolved = selection.kind === "new_chat" && selection.workspace === null;
  const workspaceCatalog = useInfiniteQuery({
    ...workspaceCatalogInfiniteQueryOptions(services.api, selection.projectID),
    enabled: selection.kind === "new_chat" && (unresolved || workspaceOpen),
    retry: false,
  });
  const restarted = useRef(false);
  const firstOffset = workspaceCatalog.data?.pages[0]?.offset;
  const defaultWorkspace = workspaceCatalog.data?.pages[0]?.workspaces.find((row) => row.isDefault);
  useEffect(() => {
    if (unresolved && firstOffset !== undefined && firstOffset > 0 && !restarted.current) {
      restarted.current = true;
      void client.resetQueries({
        exact: true,
        queryKey: queryKeys.projectWorkspaceCatalog(selection.projectID),
      });
    }
  }, [client, firstOffset, selection.projectID, unresolved]);
  useEffect(() => {
    if (!unresolved || defaultWorkspace === undefined) return;
    const resolved = updateWorkspaceSelection(
      { catalog: { state: "pending" }, initiating: undefined, selection: { state: "uncommitted" } },
      { type: "catalog-loaded", defaultWorkspace },
    );
    if (resolved.selection.state === "committed") selectWorkspace(resolved.selection.row);
  }, [defaultWorkspace, selectWorkspace, unresolved]);
  return {
    workspace: selection.kind === "new_chat" ? selection.workspace : null,
    workspaceOpen,
    setWorkspaceOpen,
    workspaceCatalog,
    workspaceUnresolved: unresolved,
    defaultWorkspaceMissing:
      unresolved && workspaceCatalog.isSuccess && firstOffset === 0 && defaultWorkspace === undefined,
  };
}
