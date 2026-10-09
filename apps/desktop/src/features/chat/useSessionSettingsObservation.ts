import { useMemo } from "react";
import { useAtomMount } from "@effect/atom-react";
import * as Atom from "effect/reactivity/Atom";
import * as Stream from "effect/Stream";
import * as Effect from "effect/Effect";
import { useTranslation } from "react-i18next";
import { errorMessage, type ChatSessionTarget } from "@/api";
import { useAppServices, useOptionalChatRuntimeOwner } from "@/app-facade";
import { showStatusToast } from "@/ui";
import type { ChatSettingsViewModel } from "./ChatSettingsViewModel";

export function useSessionSettingsObservation(
  target: ChatSessionTarget | null,
  settings: ChatSettingsViewModel,
) {
  const services = useAppServices();
  const owner = useOptionalChatRuntimeOwner();
  const { t } = useTranslation();
  const sessionID = target?.sessionID ?? null;
  const projectID = target?.projectID ?? null;
  const observation = useMemo(
    () =>
      Atom.make(
        (get) => {
          if (sessionID === null || projectID === null || owner === null) return Effect.succeed(null);
          return services.api.chat
            .subscribeSettings({ sessionID, projectID }, async () =>
              services.logger.reportObservationOverflow("session-settings"),
            )
            .pipe(
              Stream.runForEach((value) =>
                Effect.gen(function* () {
                  if (value.kind === "snapshot") {
                    yield* get.setResult(settings.receive, value.snapshot);
                    yield* Effect.promise(async () => owner.forceMainViewRead());
                  } else {
                    showStatusToast({
                      id: "chat-settings-observation",
                      tone: "danger",
                      title: t("chatSettings.operationFailed"),
                      body: errorMessage(value.error),
                    });
                  }
                }),
              ),
              Effect.as(null),
            );
        },
        { initialValue: null },
      ),
    [sessionID, projectID, owner, services, settings, t],
  );
  useAtomMount(observation);
}
