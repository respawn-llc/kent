import { type ReactNode, useEffect, useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";

import type { ChatSessionTarget } from "@/api";
import { ChatRuntimeOwner, type ChatRuntimeApi, type ChatRuntimeHost } from "./chatRuntime";
import { ChatRuntimeContext } from "./chatRuntimeContext";

export type ChatRuntimeProviderApi = Readonly<{
  chat: ChatRuntimeApi;
}>;

export type ChatRuntimeProviderProps = Readonly<{
  api: ChatRuntimeProviderApi;
  target: ChatSessionTarget | null;
  host: ChatRuntimeHost;
  children: ReactNode;
}>;

export function ChatRuntimeProvider({ api, target, host, children }: ChatRuntimeProviderProps) {
  const queryClient = useQueryClient();
  const owner = useMemo(
    () => (target === null ? null : new ChatRuntimeOwner(api.chat, target, queryClient, host)),
    [api.chat, host, queryClient, target],
  );
  useEffect(() => {
    return owner?.mount();
  }, [owner]);
  return <ChatRuntimeContext.Provider value={owner}>{children}</ChatRuntimeContext.Provider>;
}
