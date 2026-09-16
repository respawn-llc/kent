import { useEffect, useReducer, useRef } from "react";
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { errorMessage } from "@/api";
import { queryKeys, useAppServices, workspaceCatalogInfiniteQueryOptions } from "@/app-facade";
import { updateWorkspaceSelection, type WorkspaceSelectionState } from "@/shared/workspaces";
import { ErrorState, LoadingState } from "@/ui";
import { ChatDestination } from "./ChatDestination";
import type { ChatSettingsNavigation } from "./useChatSettings";

export function NewChatDestination({
  projectID,
  navigation,
  onSessionDelivered,
}: Readonly<{
  projectID: string;
  navigation: ChatSettingsNavigation;
  onSessionDelivered(sessionID: string): void;
}>) {
  const { api } = useAppServices();
  const { t } = useTranslation();
  const client = useQueryClient();
  const catalog = useInfiniteQuery({ ...workspaceCatalogInfiniteQueryOptions(api, projectID), retry: false });
  const [selection, dispatch] = useReducer(updateWorkspaceSelection, {
    catalog: { state: "pending" },
    initiating: undefined,
    selection: { state: "uncommitted" },
  } satisfies WorkspaceSelectionState);
  const restarted = useRef(false);
  const firstOffset = catalog.data?.pages[0]?.offset;
  useEffect(() => {
    if (firstOffset !== undefined && firstOffset > 0 && !restarted.current) {
      restarted.current = true;
      void client.resetQueries({ exact: true, queryKey: queryKeys.projectWorkspaceCatalog(projectID) });
    }
  }, [client, firstOffset, projectID]);
  const defaultWorkspace = catalog.data?.pages[0]?.workspaces.find((row) => row.isDefault);
  useEffect(() => {
    if (defaultWorkspace !== undefined) dispatch({ type: "catalog-loaded", defaultWorkspace });
  }, [defaultWorkspace]);
  if (selection.selection.state === "committed")
    return (
      <ChatDestination
        opening={{ kind: "new_chat", projectID, workspace: selection.selection.row }}
        navigation={navigation}
        onSessionDelivered={onSessionDelivered}
      />
    );
  if (catalog.isError || (catalog.isSuccess && firstOffset === 0 && defaultWorkspace === undefined))
    return (
      <ErrorState
        title={t("states.error")}
        body={catalog.isError ? errorMessage(catalog.error) : t("chat.defaultWorkspaceMissing")}
        retryLabel={t("app.retry")}
        onRetry={() => {
          void catalog.refetch();
        }}
      />
    );
  return <LoadingState title={t("states.loading")} />;
}
