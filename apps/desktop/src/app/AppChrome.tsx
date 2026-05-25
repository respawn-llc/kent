import { Link } from "@tanstack/react-router";
import { ChevronLeft, ChevronRight, Home, SunMoon } from "lucide-react";
import type { MouseEvent, PointerEvent, ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { toggleInMemoryThemeOverride } from "../appEnvironment";
import { appChromeTitleClassNames, appChromeTitlePlacementClassNames } from "./appChromeStyles";
import { useAppNavigation, useNavigationStackState } from "./navigation";
import { SidebarHost, SidebarRouteChangeCloser } from "./sidebar";
import { SidebarProvider } from "./sidebarProvider";
import { useAppServices } from "./useAppServices";
import { useCurrentWindowChromeTitle } from "./windowChromeTitle";
import { WorkflowEditorDraftBridgeProvider } from "../features/workflow-editor/workflowEditorDraftBridge";

export type AppChromeProps = Readonly<{
  children: ReactNode;
}>;

export function AppChrome({ children }: AppChromeProps) {
  const { t } = useTranslation();
  const { debugThemeOverrideEnabled, nativeBridge } = useAppServices();
  const navigation = useAppNavigation();
  const stack = useNavigationStackState();
  const macOS = nativeBridge.capabilities.platform === "macos";
  const title = useCurrentWindowChromeTitle();

  return (
    <main className="window-glass-fill grid h-screen w-screen overflow-hidden pt-[var(--native-titlebar-height)]">
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
        {stack.hasHistory && !macOS ? (
          <HistoryButtons
            backLabel={t("app.back")}
            forwardLabel={t("app.forward")}
            navigation={navigation}
            placement="before-home"
            stack={stack}
          />
        ) : null}
        <Link
          aria-label={t("app.home")}
          className="grid h-6 w-6 place-items-center rounded-full border border-transparent text-[var(--color-on-island)]"
          onClick={(event) => {
            if (isPlainPrimaryClick(event)) {
              event.preventDefault();
              void navigation.openHome();
            }
          }}
          to="/"
        >
          <Home aria-hidden="true" size={16} strokeWidth={1.125} />
        </Link>
        {stack.hasHistory && macOS ? (
          <HistoryButtons
            backLabel={t("app.back")}
            forwardLabel={t("app.forward")}
            navigation={navigation}
            placement="after-home"
            stack={stack}
          />
        ) : null}
        {debugThemeOverrideEnabled ? <DebugThemeToggle label={t("app.toggleTheme")} /> : null}
      </div>
      {title !== null ? (
        <div
          className={[...appChromeTitleClassNames, ...appChromeTitlePlacementClassNames(macOS)].join(" ")}
          data-testid="app-chrome-title"
        >
          {title}
        </div>
      ) : null}
      <SidebarProvider>
        <WorkflowEditorDraftBridgeProvider>
          <div
            className="app-region-no-drag relative flex min-h-0 min-w-0 w-full overflow-hidden"
            data-testid="app-shell-content"
          >
            <div className="min-h-0 min-w-0 flex-1 overflow-visible" data-testid="app-main-content">
              {children}
            </div>
            <SidebarHost />
          </div>
          <SidebarRouteChangeCloser />
        </WorkflowEditorDraftBridgeProvider>
      </SidebarProvider>
    </main>
  );
}

function isPlainPrimaryClick(event: MouseEvent): boolean {
  return event.button === 0 && !event.altKey && !event.ctrlKey && !event.metaKey && !event.shiftKey;
}

function DebugThemeToggle({ label }: Readonly<{ label: string }>) {
  return (
    <button
      aria-label={label}
      className="grid h-6 w-6 place-items-center rounded-full border border-transparent bg-transparent text-[var(--color-on-island)]"
      data-testid="app-chrome-debug-theme-toggle"
      onClick={() => {
        toggleInMemoryThemeOverride();
      }}
      type="button"
    >
      <SunMoon aria-hidden="true" size={16} strokeWidth={1.25} />
    </button>
  );
}

function HistoryButtons({
  navigation,
  stack,
  backLabel,
  forwardLabel,
  placement,
}: Readonly<{
  backLabel: string;
  forwardLabel: string;
  navigation: ReturnType<typeof useAppNavigation>;
  placement: "before-home" | "after-home";
  stack: ReturnType<typeof useNavigationStackState>;
}>) {
  return (
    <div className="grid grid-cols-2" data-placement={placement} data-testid="app-chrome-history-buttons">
      <button
        aria-label={backLabel}
        className="grid h-6 w-6 place-items-center rounded-full border border-transparent bg-transparent text-[var(--color-on-island)] disabled:opacity-35"
        disabled={!stack.canGoBack}
        onClick={() => void navigation.back()}
        type="button"
      >
        <ChevronLeft aria-hidden="true" size={16} strokeWidth={1.25} />
      </button>
      <button
        aria-label={forwardLabel}
        className="grid h-6 w-6 place-items-center rounded-full border border-transparent bg-transparent text-[var(--color-on-island)] disabled:opacity-35"
        disabled={!stack.canGoForward}
        onClick={() => void navigation.forward()}
        type="button"
      >
        <ChevronRight aria-hidden="true" size={16} strokeWidth={1.25} />
      </button>
    </div>
  );
}

async function startNativeWindowDrag(
  event: PointerEvent<HTMLDivElement>,
  startDragging: () => Promise<void>,
): Promise<void> {
  if (event.button !== 0) {
    return;
  }
  event.preventDefault();
  await startDragging();
}
