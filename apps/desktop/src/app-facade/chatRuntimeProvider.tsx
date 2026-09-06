import { type ReactNode, useEffect, useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";

import type { ApiConnectionSource, ChatSessionTarget } from "@/api";
import { ChatRuntimeOwner, type ChatRuntimeApi, type ChatRuntimeHost } from "./chatRuntime";
import { ChatRuntimeContext } from "./chatRuntimeContext";

export type ChatRuntimeProviderApi = Readonly<{
  connection: ApiConnectionSource;
  chat: ChatRuntimeApi;
}>;

export type ChatRuntimeProviderProps = Readonly<{
  api: ChatRuntimeProviderApi;
  target: ChatSessionTarget;
  host: ChatRuntimeHost;
  children: ReactNode;
}>;

export function ChatRuntimeProvider({ api, target, host, children }: ChatRuntimeProviderProps) {
  const queryClient = useQueryClient();
  const owner = useMemo(
    () => new ChatRuntimeOwner(api.chat, target, queryClient, host),
    [api.chat, host, queryClient, target],
  );
  useEffect(() => {
    owner.start();
    return () => {
      void owner.dispose();
    };
  }, [owner]);
  useEffect(() => {
    let armed = false;
    const observe = () => {
      const connection = api.connection.snapshot();
      if (connection.phase === "disconnected") {
        armed = true;
        return;
      }
      if (connection.phase === "connected" && armed) {
        armed = false;
        owner.controlReconnected();
      }
    };
    observe();
    return api.connection.subscribe(observe);
  }, [api, owner]);
  return <ChatRuntimeContext.Provider value={owner}>{children}</ChatRuntimeContext.Provider>;
}
