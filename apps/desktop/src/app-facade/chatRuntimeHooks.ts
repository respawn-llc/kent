import { useContext } from "react";
import { useAtomValue, useAtomSet } from "@effect/atom-react";
import * as Atom from "effect/reactivity/Atom";
import * as Effect from "effect/Effect";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import type { ChatMainView } from "@/api";
import { sessionDisplayTitle } from "@/api";

import { ChatRuntimeContext } from "./chatRuntimeContext";
import type { ChatRuntimeOwner, ChatRuntimeOwnerSnapshot } from "./chatRuntime";
import type { ChatTranscriptHostInput } from "./chatTranscriptHost";

const absentSnapshot = Atom.make<ChatRuntimeOwnerSnapshot | null>(null);
const absentMetadata = Atom.make<Atom.Type<ChatRuntimeOwner["metadata"]> | null>(null);
const absentAction = Atom.fn(() => Effect.void);
const absentDispatch = Atom.fn<ChatTranscriptHostInput>()(() => Effect.void);
const absentJump = Atom.fn<{ onReady(): void; onError(error: Error): void }>()(() => Effect.void);

export function useChatRuntimeOwner(): ChatRuntimeOwner {
  const owner = useOptionalChatRuntimeOwner();
  if (owner === null) throw new Error("Chat Runtime hooks require ChatRuntimeProvider.");
  return owner;
}

export function useOptionalChatRuntimeOwner(): ChatRuntimeOwner | null {
  return useContext(ChatRuntimeContext);
}

export function useChatRuntimeSnapshot(): ChatRuntimeOwnerSnapshot {
  const snapshot = useOptionalChatRuntimeSnapshot();
  if (snapshot === null) throw new Error("Chat Runtime hooks require ChatRuntimeProvider.");
  return snapshot;
}

export function useOptionalChatRuntimeSnapshot(): ChatRuntimeOwnerSnapshot | null {
  const owner = useOptionalChatRuntimeOwner();
  return useAtomValue(owner?.snapshot ?? absentSnapshot);
}

export function useChatMainViewState() {
  const owner = useChatRuntimeOwner();
  const query = useAtomValue(owner.metadata);
  const retry = useAtomSet(owner.forceMainViewRead);
  return {
    data: query.data,
    error: query.error,
    status: query.status,
    fetchStatus: query.fetchStatus,
    isFetching: query.isFetching,
    retry: () => {
      retry(undefined);
    },
  } as const;
}

export function useChatExecutionTarget() {
  return useChatMainViewState().data?.executionTarget ?? null;
}

export function useChatRuntimeActivity() {
  return useChatMainViewState().data?.activity ?? null;
}

function processCount(snapshot: ChatRuntimeOwnerSnapshot | null): number {
  return snapshot === null ? 0 : snapshot.activeProcessCount;
}

function presentationTitle(view: ChatMainView | undefined, t: TFunction): string | null {
  if (view === undefined) return null;
  if (view.sessionName !== null || view.category === "main") return view.sessionName;
  return sessionDisplayTitle(
    { name: view.sessionName, id: view.sessionID, shortID: view.shortID, category: view.category },
    (id) => t("home.subagentTitle", { id }),
  );
}

export function useChatRuntimePresentation() {
  const { t } = useTranslation();
  const owner = useContext(ChatRuntimeContext);
  const snapshot = useAtomValue(owner?.snapshot ?? absentSnapshot);
  const query = useAtomValue(owner?.metadata ?? absentMetadata);
  const retry = useAtomSet(owner?.forceMainViewRead ?? absentAction);
  const data = query === null ? undefined : query.data;
  return {
    activeProcessCount: processCount(snapshot),
    activity: data?.activity ?? null,
    sessionName: presentationTitle(data, t),
    ...projectionPresentation(snapshot),
    mainView: mainViewPresentation(query, () => {
      retry(undefined);
    }),
  };
}

function mainViewPresentation(query: Atom.Type<ChatRuntimeOwner["metadata"]> | null, retry: () => void) {
  return query === null
    ? { kind: "absent" as const }
    : {
        kind: "session" as const,
        data: query.data,
        status: query.status,
        error: query.error,
        fetchStatus: query.fetchStatus,
        retry,
      };
}

function projectionPresentation(snapshot: ChatRuntimeOwnerSnapshot | null) {
  return {
    goal: snapshot?.goal.kind === "observed" ? snapshot.goal.value : null,
    observationError: snapshot?.observation.kind === "error" ? snapshot.observation.error : null,
  };
}

export function useChatTranscriptActions() {
  const owner = useOptionalChatRuntimeOwner();
  return {
    dispatch: useAtomSet(owner?.transcript.dispatch ?? absentDispatch),
    jump: useAtomSet(owner?.transcript.jump ?? absentJump),
    retry: useAtomSet(owner?.retryTranscriptObservation ?? absentAction),
  };
}
