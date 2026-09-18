import { useCallback, useMemo } from "react";
import { useAtomMount, useAtomSet, useAtomValue } from "@effect/atom-react";

import type { PendingWorkIdentity } from "@/api";
import { useChatRuntimePresentation, type ChatRuntimeHost } from "@/app-facade";
import type { ComposerPendingViewModel } from "./ComposerPendingViewModel";
import type { ComposerTextRestoration } from "./ComposerDraftViewModel";

export function useComposerPendingWork(
  model: ComposerPendingViewModel,
  restore: (input: ComposerTextRestoration) => void,
) {
  const { mainView } = useChatRuntimePresentation();
  const query = useAtomValue(model.read);
  useAtomMount(model.stopping);
  useAtomMount(model.discarding);
  const stopPending = useAtomValue(model.stopPending);
  const refreshAction = useAtomSet(model.refresh);
  const hydrate = useAtomSet(model.hydrate);
  const stopAction = useAtomSet(model.stop);
  const discardAction = useAtomSet(model.discard);
  const refresh = useCallback(() => {
    refreshAction(undefined);
  }, [refreshAction]);
  const observation = useMemo<
    Pick<
      ChatRuntimeHost,
      "onPendingWorkHydrated" | "onPendingWorkChanged" | "onPendingWorkRestored" | "onHumanInputInterrupted"
    >
  >(
    () => ({
      onPendingWorkHydrated: () => {
        hydrate(undefined);
      },
      onPendingWorkChanged: refresh,
      onPendingWorkRestored: (event) => {
        restore({ text: event.Restoration.CanonicalInput, direction: "prepend" });
      },
      onHumanInputInterrupted: (messages) => {
        restore({ text: messages.map((message) => message.Text).join("\n"), direction: "prepend" });
      },
    }),
    [hydrate, refresh, restore],
  );
  return {
    query,
    items: query.data?.items ?? [],
    observation,
    refresh,
    discard: (item: PendingWorkIdentity) => {
      discardAction({ item, restore, refresh });
    },
    discardPending: model.discardPending,
    stop: () => {
      stopAction({
        onSettled: () => {
          if (mainView.kind === "session") void mainView.retry();
        },
      });
    },
    stopPending,
  };
}
