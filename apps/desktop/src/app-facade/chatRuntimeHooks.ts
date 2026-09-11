import { useContext, useSyncExternalStore } from "react";
import { useQuery } from "@tanstack/react-query";

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
