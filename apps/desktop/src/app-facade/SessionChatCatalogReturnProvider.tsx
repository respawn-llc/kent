import { useRouter } from "@tanstack/react-router";
import { useMemo, type ReactNode } from "react";
import { useAtomSet, useAtomSuspense, useAtomValue } from "@effect/atom-react";
import * as Effect from "effect/Effect";
import * as Stream from "effect/Stream";
import * as Atom from "effect/unstable/reactivity/Atom";

import { SessionChatCatalogReturnContext, type SessionChatCatalogReturn } from "./sessionChatCatalogReturn";
import { sessionChatHistoryStateSchema } from "./sessionChatHistory";
import { NavigationStackContext, navigationHistoryChanges, nextReachableHistoryIndex } from "./navigation";
import { shellObservationDiagnostics } from "./shellObservationDiagnostics";
import { useAppServices } from "./useAppServices";

type SessionChatHistoryRead =
  | Readonly<{ kind: "absent" }>
  | Readonly<{ kind: "withoutCatalog" }>
  | Readonly<{ kind: "withCatalog"; value: SessionChatCatalogReturn }>;

export function SessionChatCatalogReturnProvider({ children }: Readonly<{ children: ReactNode }>) {
  const router = useRouter();
  const { logger } = useAppServices();
  const model = useMemo(() => {
    const historyRead = readSessionChatHistory(router.history.location.state);
    const state = Atom.make<
      Readonly<{
        currentIndex: number;
        maxReachableIndex: number;
        catalogReturn: SessionChatCatalogReturn | null;
      }>
    >({
      currentIndex: router.history.location.state.__TSR_index,
      maxReachableIndex: router.history.location.state.__TSR_index,
      catalogReturn: historyRead.kind === "withCatalog" ? historyRead.value : null,
    });
    const observation = Atom.make(
      (get) =>
        navigationHistoryChanges(router.history, shellObservationDiagnostics(logger, "history")).pipe(
          Stream.runForEach(({ location, action }) =>
            Effect.sync(() => {
              const previous = get.once(state);
              const read = readSessionChatHistory(location.state);
              get.set(state, {
                currentIndex: location.state.__TSR_index,
                maxReachableIndex: nextReachableHistoryIndex(
                  previous.maxReachableIndex,
                  action.type,
                  location.state.__TSR_index,
                ),
                catalogReturn:
                  read.kind === "withCatalog"
                    ? read.value
                    : read.kind === "withoutCatalog"
                      ? null
                      : previous.catalogReturn,
              });
            }),
          ),
          Effect.as(null),
        ),
      { initialValue: null },
    );
    return { state, observation };
  }, [router.history, logger]);
  useAtomSuspense(model.observation);
  const state = useAtomValue(model.state);
  const setState = useAtomSet(model.state);
  const canGoBack = state.currentIndex > 0;
  const canGoForward = state.currentIndex < state.maxReachableIndex;

  return (
    <NavigationStackContext.Provider
      value={{ canGoBack, canGoForward, hasHistory: canGoBack || canGoForward }}
    >
      <SessionChatCatalogReturnContext.Provider
        value={{
          catalogReturn: state.catalogReturn,
          consume: (projectID) => {
            setState((current) =>
              current.catalogReturn?.projectID === projectID ? { ...current, catalogReturn: null } : current,
            );
          },
        }}
      >
        {children}
      </SessionChatCatalogReturnContext.Provider>
    </NavigationStackContext.Provider>
  );
}

function readSessionChatHistory(state: unknown): SessionChatHistoryRead {
  const parsed = sessionChatHistoryStateSchema.safeParse(state);
  if (!parsed.success || !Object.hasOwn(parsed.data, "sessionChat")) {
    return { kind: "absent" };
  }
  const sessionChat = parsed.data.sessionChat;
  const projectID = sessionChat?.projectID;
  const catalogOrigin = sessionChat?.catalogOrigin;
  if (projectID === undefined || catalogOrigin === undefined || catalogOrigin === null) {
    return { kind: "withoutCatalog" };
  }
  return {
    kind: "withCatalog",
    value: {
      category: catalogOrigin.category,
      projectID,
    },
  };
}
