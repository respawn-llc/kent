import { useContext, useSyncExternalStore } from "react";
import { skipToken, useQuery } from "@tanstack/react-query";
import type { ChatMainView } from "@/api";

import { ChatRuntimeContext } from "./chatRuntimeContext";
import type { ChatRuntimeOwner, ChatRuntimeOwnerSnapshot } from "./chatRuntime";

export function useChatRuntimeOwner(): ChatRuntimeOwner {
  const owner = useContext(ChatRuntimeContext);
  if (owner === null) throw new Error("Chat Runtime hooks require ChatRuntimeProvider.");
  return owner;
}

export function useChatRuntimeSnapshot(): ChatRuntimeOwnerSnapshot {
  const owner = useChatRuntimeOwner();
  return useSyncExternalStore(
    (listener) => owner.subscribe(listener),
    () => owner.snapshot,
    () => owner.snapshot,
  );
}

export function useChatMainViewState() {
  const owner = useChatRuntimeOwner();
  const query = useQuery(owner.mainViewOptions());
  return {
    data: query.data,
    error: query.error,
    status: query.status,
    fetchStatus: query.fetchStatus,
    isFetching: query.isFetching,
    retry: async () => owner.retryMainView(),
  } as const;
}

export function useChatExecutionTarget() {
  return useChatMainViewState().data?.executionTarget ?? null;
}

export function useChatRuntimeActivity() {
  return useChatMainViewState().data?.activity ?? null;
}

export function useChatRuntimePresentation() {
  const owner = useContext(ChatRuntimeContext);
  const snapshot = useSyncExternalStore(
    (listener) => owner?.subscribe(listener) ?? (() => undefined),
    () => owner?.snapshot ?? null,
    () => owner?.snapshot ?? null,
  );
  const query = useQuery<ChatMainView>(
    owner?.mainViewOptions() ?? {
      queryKey: ["chat-no-session"],
      queryFn: skipToken,
    },
  );
  return {
    activity: query.data?.activity ?? null,
    sessionName: query.data?.sessionName ?? null,
    goal: snapshot?.goal.kind === "observed" ? snapshot.goal.value : null,
    observationError: snapshot?.observation.kind === "error" ? snapshot.observation.error : null,
    mainView:
      owner === null
        ? { kind: "absent" as const }
        : {
            kind: "session" as const,
            data: query.data,
            status: query.status,
            error: query.error,
            fetchStatus: query.fetchStatus,
            retry: async () => owner.retryMainView(),
          },
  };
}
