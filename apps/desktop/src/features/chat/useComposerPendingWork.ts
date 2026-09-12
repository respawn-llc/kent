import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import {
  errorMessage,
  type ChatSessionTarget,
  type ChatSettingsTarget,
  type PendingWorkIdentity,
  type PendingWorkItem,
} from "@/api";
import { useAppServices, type ChatRuntimeHost } from "@/app-facade";
import { showStatusToast } from "@/ui";

type ReadScope = Readonly<{ target: ChatSessionTarget }>;

export function useComposerPendingWork(
  target: ChatSettingsTarget,
  restore: (text: string, direction: "append" | "prepend") => void,
) {
  const { api } = useAppServices();
  const { t } = useTranslation();
  const [items, setItems] = useState<readonly PendingWorkItem[]>([]);
  const [discarding, setDiscarding] = useState<ReadonlyMap<symbol, PendingWorkIdentity>>(new Map());
  const [stopRequests, setStopRequests] = useState(0);
  const scope = useRef<ReadScope | null>(null);
  const report = useCallback(
    (error: unknown) => {
      showStatusToast({
        id: "chat-pending-work",
        tone: "danger",
        title: t("chatComposer.pendingFailed"),
        body: errorMessage(error),
      });
    },
    [t],
  );
  const read = useCallback(
    (captured: ReadScope) => {
      void api.chat.listPendingWork(captured.target).then(
        (result) => {
          if (scope.current === captured) setItems(result.items);
        },
        (error: unknown) => {
          if (scope.current === captured) report(error);
        },
      );
    },
    [api.chat, report],
  );
  const beginScope = useCallback(
    (session: ChatSessionTarget) => {
      const next: ReadScope = { target: session };
      scope.current = next;
      read(next);
    },
    [read],
  );
  const hydrate = useCallback(
    (session: ChatSessionTarget) => {
      setItems([]);
      beginScope(session);
    },
    [beginScope],
  );
  const refresh = useCallback(
    (session: ChatSessionTarget) => {
      const current = scope.current;
      if (current?.target.sessionID !== session.sessionID) {
        hydrate(session);
      } else read(current);
    },
    [hydrate, read],
  );
  useEffect(() => {
    if (target.kind === "session") beginScope(target);
    return () => {
      scope.current = null;
    };
  }, [target, beginScope]);
  const observation = useMemo<
    Pick<
      ChatRuntimeHost,
      "onPendingWorkHydrated" | "onPendingWorkChanged" | "onPendingWorkRestored" | "onHumanInputInterrupted"
    >
  >(
    () => ({
      onPendingWorkHydrated: (sessionID) => {
        hydrate({ ...target, sessionID });
      },
      onPendingWorkChanged: () => {
        if (scope.current !== null) read(scope.current);
      },
      onPendingWorkRestored: (event) => {
        restore(event.Restoration.CanonicalInput, "prepend");
      },
      onHumanInputInterrupted: (messages) => {
        restore(messages.map((message) => message.Text).join("\n"), "prepend");
      },
    }),
    [target, hydrate, read, restore],
  );
  async function discard(item: PendingWorkIdentity) {
    const current = scope.current;
    if (current === null) return;
    const request = Symbol();
    setDiscarding((active) => new Map(active).set(request, item));
    try {
      const restoration = await api.chat.removePendingWork(current.target, item);
      restore(restoration.canonicalInput, "append");
      refresh(current.target);
    } catch (error) {
      report(error);
    } finally {
      setDiscarding((active) => {
        const remaining = new Map(active);
        remaining.delete(request);
        return remaining;
      });
    }
  }
  async function stop() {
    if (target.kind !== "session") return;
    setStopRequests((count) => count + 1);
    try {
      await api.chat.stop(target);
    } catch (error) {
      report(error);
    } finally {
      setStopRequests((count) => count - 1);
    }
  }
  return { items, observation, refresh, discard, discarding, stop, stopRequests };
}
