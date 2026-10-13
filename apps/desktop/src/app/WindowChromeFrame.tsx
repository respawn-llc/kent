import { useMemo, type PointerEvent, type ReactNode } from "react";
import { useAtomMount } from "@effect/atom-react";
import * as Atom from "effect/reactivity/Atom";
import * as Effect from "effect/Effect";
import { useTranslation } from "react-i18next";
import { useAppServices, useCurrentWindowChromeTitle } from "@/app-facade";
import { appChromeTopTreatmentForPlatform } from "./appChromeStyles";
import { WindowChromeTitle } from "./WindowChromeTitle";

export function WindowChromeFrame({
  children,
  controls,
  floatingControls,
  defaultTitle,
}: Readonly<{
  children: ReactNode;
  controls?: ReactNode;
  floatingControls?: ReactNode;
  defaultTitle?: string;
}>) {
  const { nativeBridge, logger } = useAppServices();
  const { t } = useTranslation();
  const macOS = nativeBridge.capabilities.platform === "macos";
  const topTreatment = appChromeTopTreatmentForPlatform(nativeBridge.capabilities.platform);
  const title = useCurrentWindowChromeTitle();
  const nativeTitle = title ?? defaultTitle ?? t("app.title");
  const titleSync = useMemo(
    () =>
      Atom.make(
        Effect.tryPromise(() => nativeBridge.window.setTitle(nativeTitle)).pipe(
          Effect.catch((error) =>
            Effect.promise(() =>
              logger.append("warn", "Native window title update failed.", { error: String(error.cause) }),
            ),
          ),
        ),
      ),
    [nativeBridge, logger, nativeTitle],
  );
  useAtomMount(titleSync);
  return (
    <main className="window-glass-fill grid h-screen w-screen overflow-hidden pt-[var(--native-titlebar-height)]">
      <div
        aria-hidden="true"
        className={topTreatment.classNames.join(" ")}
        data-effect={topTreatment.effect}
        data-testid="app-chrome-top-treatment"
        style={topTreatment.style}
      />
      <div
        className="app-region-drag fixed inset-x-0 top-0 z-20 h-[var(--native-titlebar-height)]"
        data-tauri-drag-region
        onPointerDown={(event) => {
          void startNativeWindowDrag(event, nativeBridge.window.startDragging);
        }}
      />
      <div
        className={`app-region-no-drag fixed top-[8px] z-30 flex h-6 items-center ${macOS ? "left-[var(--native-home-link-left-macos)]" : "right-[var(--space-4)]"}`}
        data-testid="app-chrome-navigation"
      >
        {controls}
        {title !== null && macOS ? <WindowChromeTitle title={title} inline macOS /> : null}
      </div>
      {floatingControls}
      {title !== null && !macOS ? <WindowChromeTitle title={title} macOS={false} /> : null}
      <div
        className="app-region-no-drag relative flex min-h-0 min-w-0 w-full overflow-clip"
        data-testid="app-shell-content"
      >
        {children}
      </div>
    </main>
  );
}

async function startNativeWindowDrag(
  event: PointerEvent<HTMLDivElement>,
  startDragging: () => Promise<void>,
): Promise<void> {
  if (event.button !== 0) return;
  event.preventDefault();
  await startDragging();
}
