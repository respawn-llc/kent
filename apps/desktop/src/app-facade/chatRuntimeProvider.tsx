import { type ReactNode, useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useAtomMount, useAtomSuspense } from "@effect/atom-react";
import * as Atom from "effect/reactivity/Atom";
import * as Effect from "effect/Effect";

import type { ChatSessionTarget } from "@/api";
import { createChatRuntimeModel, type ChatRuntimeApi, type ChatRuntimeHost } from "./chatRuntime";
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

const absentOwner = Atom.make(null);
const absentDiagnostics = Atom.make(Effect.succeed(null), { initialValue: null });

export function ChatRuntimeProvider({ api, target, host, children }: ChatRuntimeProviderProps) {
  const queryClient = useQueryClient();
  const sessionID = target?.sessionID ?? null;
  const projectID = target?.projectID ?? null;
  const owner = useMemo(
    () =>
      sessionID === null || projectID === null
        ? null
        : createChatRuntimeModel(api.chat, { sessionID, projectID }, queryClient, host),
    [api.chat, host, queryClient, sessionID, projectID],
  );
  useAtomMount(owner?.mount ?? absentOwner);
  useAtomSuspense(owner?.diagnostics ?? absentDiagnostics);
  return <ChatRuntimeContext.Provider value={owner}>{children}</ChatRuntimeContext.Provider>;
}
