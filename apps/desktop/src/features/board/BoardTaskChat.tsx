import { createContext, useContext, useMemo, useState } from "react";
import { QueryObserver, useQueryClient } from "@tanstack/react-query";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import * as Atom from "effect/reactivity/Atom";
import * as Effect from "effect/Effect";
import { queryAtom, queryKeys, useAppServices } from "@/app-facade";

type BoardTaskChatNavigation = Readonly<{
  openSession(sessionID: string): Promise<void>;
  report(error: unknown): void;
}>;

export const BoardTaskChatContext = createContext<BoardTaskChatNavigation | null>(null);

const openSessionChat = (navigation: BoardTaskChatNavigation, sessionID: string) =>
  Effect.tryPromise({
    try: async () => navigation.openSession(sessionID),
    catch: (cause) => ({ _tag: "BoardChatNavigationError" as const, cause }),
  }).pipe(
    Effect.catch((error) =>
      Effect.sync(() => {
        navigation.report(error.cause);
      }),
    ),
  );

export function useBoardTaskChat(taskID: string) {
  const navigation = useContext(BoardTaskChatContext);
  if (navigation === null) throw new Error("Board Task chat requires its Board navigation owner");
  const services = useAppServices();
  const client = useQueryClient();
  const [pickerOpen, setPickerOpen] = useState(false);
  const model = useMemo(() => {
    const observer = new QueryObserver(client, {
      queryKey: queryKeys.task(taskID),
      queryFn: async () => services.api.getTask(taskID),
      enabled: false,
      retry: false,
    });
    const detail = queryAtom(observer);
    const request = Atom.fn<
      Readonly<{
        navigation: BoardTaskChatNavigation;
        showPicker(): void;
      }>
    >()(
      (input) =>
        Effect.gen(function* () {
          if (observer.getCurrentResult().isFetching) return;
          const result = yield* Effect.promise(async () => observer.refetch());
          if (result.isError) {
            input.navigation.report(result.error);
            return;
          }
          const sessions = result.data?.liveSessions;
          if (sessions === undefined || sessions.length === 0) return;
          const only = sessions.length === 1 ? sessions[0] : undefined;
          if (only === undefined) {
            input.showPicker();
          } else {
            yield* openSessionChat(input.navigation, only.sessionID);
          }
        }),
      { concurrent: true },
    );
    const select = Atom.fn<Readonly<{ navigation: BoardTaskChatNavigation; sessionID: string }>>()(
      (input) => openSessionChat(input.navigation, input.sessionID),
      { concurrent: true },
    );
    return { detail, request, select };
  }, [client, services, taskID]);
  const detail = useAtomValue(model.detail);
  const requestState = useAtomValue(model.request);
  const selectState = useAtomValue(model.select);
  const request = useAtomSet(model.request);
  const select = useAtomSet(model.select);
  return {
    pickerOpen,
    setPickerOpen,
    sessions: detail.data?.liveSessions ?? [],
    pending: detail.isFetching || requestState.waiting || selectState.waiting,
    open: () => {
      request({
        navigation,
        showPicker: () => {
          setPickerOpen(true);
        },
      });
    },
    select: (sessionID: string) => {
      setPickerOpen(false);
      select({ navigation, sessionID });
    },
  };
}
