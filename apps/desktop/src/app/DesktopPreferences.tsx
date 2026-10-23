import { useMemo, useLayoutEffect, type ReactNode } from "react";
import { useAtomMount } from "@effect/atom-react";
import { useQueryClient } from "@tanstack/react-query";
import * as Atom from "effect/reactivity/Atom";
import * as Effect from "effect/Effect";
import * as Stream from "effect/Stream";
import { useTranslation } from "react-i18next";
import {
  desktopSettingsKey,
  effectiveAppearance,
  useAppServices,
  useDesktopSettings,
  useStatusController,
} from "@/app-facade";
import { ReducedMotionContext, useSystemReducedMotion } from "@/ui";
import { errorMessage } from "@/api";
import { applyConfiguredTheme } from "./startup/appEnvironment";

export function DesktopPreferences({ children }: Readonly<{ children: ReactNode }>) {
  const services = useAppServices();
  const client = useQueryClient();
  const settings = useDesktopSettings();
  const systemReducedMotion = useSystemReducedMotion();
  const appearance = effectiveAppearance(settings, systemReducedMotion);
  const { push } = useStatusController();
  const { t } = useTranslation();
  const observation = useMemo(
    () =>
      Atom.make((get) =>
        Stream.merge(
          Stream.succeed(undefined),
          services.nativeBridge.settings.changes(async () =>
            services.logger.reportObservationOverflow("desktop-settings"),
          ),
        ).pipe(
          Stream.debounce("16 millis"),
          Stream.runForEach(() =>
            Effect.tryPromise(async () => {
              const snapshot = await services.nativeBridge.settings.read();
              client.setQueryData(desktopSettingsKey, snapshot);
            }),
          ),
          Effect.catch((error) =>
            Effect.sync(() => {
              push({
                id: "desktop-settings-observation",
                tone: "danger",
                title: t("settings.title"),
                body: errorMessage(error),
                actionLabel: t("app.retry"),
                onAction: () => {
                  get.refreshSelf();
                },
              });
            }),
          ),
        ),
      ),
    [services, client, push, t],
  );
  useAtomMount(observation);
  useLayoutEffect(() => {
    applyConfiguredTheme(settings.saved.theme);
    document.documentElement.dataset.reducedMotion = String(appearance.reducedMotion);
    document.documentElement.dataset.lowPower = String(appearance.lowPower);
    document.documentElement.dataset.opaque = String(
      services.nativeBridge.capabilities.platform !== "browser" && !appearance.transparent,
    );
  }, [settings.saved.theme, appearance.reducedMotion, appearance.lowPower, appearance.transparent, services]);
  return (
    <ReducedMotionContext.Provider value={appearance.reducedMotion}>{children}</ReducedMotionContext.Provider>
  );
}
