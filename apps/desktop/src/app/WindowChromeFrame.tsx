import type { PointerEvent, ReactNode } from "react";
import { useAppServices, useCurrentWindowChromeTitle } from "@/app-facade";
import { appChromeTopTreatmentForPlatform } from "./appChromeStyles";
import { WindowChromeTitle } from "./WindowChromeTitle";

export function WindowChromeFrame({
  children,
  controls,
  floatingControls,
}: Readonly<{ children: ReactNode; controls?: ReactNode; floatingControls?: ReactNode }>) {
  const { nativeBridge } = useAppServices();
  const macOS = nativeBridge.capabilities.platform === "macos";
  const topTreatment = appChromeTopTreatmentForPlatform(nativeBridge.capabilities.platform);
  const title = useCurrentWindowChromeTitle();
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
        className="app-region-no-drag relative flex min-h-0 min-w-0 w-full overflow-hidden"
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
