import { useMemo } from "react";
import { useAtomSuspense } from "@effect/atom-react";
import * as Atom from "effect/reactivity/Atom";
import * as Effect from "effect/Effect";
import * as Stream from "effect/Stream";
import { useTranslation } from "react-i18next";

import { errorMessage } from "@/api";
import { useAppNavigation, useAppServices, useStatusController } from "@/app-facade";
import { useStableCallback } from "@/ui";
import type { NativeMainNavigation } from "@app/native-bridge";

export function MainWindowNavigation() {
  const { nativeBridge, logger } = useAppServices();
  const navigation = useAppNavigation();
  const { push } = useStatusController();
  const { t } = useTranslation();
  const reportFailure = useStableCallback((error: unknown) => {
    push({
      id: "main-window-navigation-error",
      tone: "danger",
      title: t("states.error"),
      body: errorMessage(error),
    });
  });
  const handle = useStableCallback(async (destination: NativeMainNavigation) => {
    await nativeBridge.window.focusMain();
    if (destination.kind === "sessionChat") {
      await navigation.openSessionChat(destination);
    } else {
      await navigation.openTask(destination.taskID);
    }
  });
  const model = useMemo(() => {
    const action = Atom.fn<NativeMainNavigation>()(
      (destination) =>
        Effect.tryPromise(async () => handle(destination)).pipe(
          Effect.catch((error) =>
            Effect.sync(() => {
              reportFailure(error.cause);
            }),
          ),
          Effect.as(null),
        ),
      { concurrent: true, initialValue: null },
    );
    const requests = Atom.make(
      (get) =>
        nativeBridge.window
          .mainNavigationRequests(async () => logger.reportObservationOverflow("main-navigation"))
          .pipe(
            Stream.runForEach((destination) =>
              Effect.sync(() => {
                get.set(action, destination);
              }),
            ),
            Effect.catch((error) =>
              Effect.sync(() => {
                reportFailure(error);
              }),
            ),
            Effect.as(null),
          ),
      { initialValue: null },
    );
    return { action, requests };
  }, [nativeBridge.window, logger, handle, reportFailure]);
  useAtomSuspense(model.action);
  useAtomSuspense(model.requests);
  return null;
}
