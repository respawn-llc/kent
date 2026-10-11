import { Link, useLocation } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight, Home } from "lucide-react";
import { useCallback, type MouseEvent, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { TaskSearchGlobalTrigger, TaskSearchHost, TaskSearchProvider } from "@/features/board";
import { AttentionController } from "./AttentionController";
import { WindowChromeThemeToggle } from "./WindowChromeThemeToggle";
import { AppUpdateChip } from "./AppUpdateChip";
import { useDesktopUpdate, type DesktopUpdateState } from "./useDesktopUpdate";
import { WindowChromeFrame } from "./WindowChromeFrame";
import { MainWindowNavigation } from "./MainWindowNavigation";
import { SessionChatCatalogReturnProvider, useAppNavigation, useNavigationStackState } from "@/app-facade";
import { completeProjectDeletion, useProjectDeletedEvents } from "@/app-facade";
import { SidebarHost } from "./sidebar";
import { SidebarProvider } from "./sidebarProvider";
import { sidebarDestinationPolicy } from "./sidebarDestinationPolicy";
import { useStatusController } from "@/app-facade";
import { useAppServices } from "@/app-facade";
import { useCurrentWindowChromeAction } from "@/app-facade";

export type AppChromeProps = Readonly<{
  children: ReactNode;
}>;

export function AppChrome({ children }: AppChromeProps) {
  return (
    <TaskSearchProvider>
      <SidebarProvider policy={sidebarDestinationPolicy}>
        <SessionChatCatalogReturnProvider>
          <AppChromeContent>{children}</AppChromeContent>
        </SessionChatCatalogReturnProvider>
      </SidebarProvider>
    </TaskSearchProvider>
  );
}

function AppChromeContent({ children }: AppChromeProps) {
  const { t } = useTranslation();
  const { nativeBridge, logger } = useAppServices();
  const navigation = useAppNavigation();
  const stack = useNavigationStackState();
  const macOS = nativeBridge.capabilities.platform === "macos";
  const action = useCurrentWindowChromeAction();
  const update = useDesktopUpdate(nativeBridge, logger);
  return (
    <WindowChromeFrame
      floatingControls={<AppChromeFloatingUpdateChip state={update} visible={macOS} />}
      controls={
        <>
          <AppChromeGlobalSearch macOS={macOS} position="leading" />
          {stack.hasHistory && !macOS ? (
            <HistoryButtons
              backLabel={t("app.back")}
              forwardLabel={t("app.forward")}
              navigation={navigation}
              placement="before-home"
              stack={stack}
            />
          ) : null}
          {!macOS ? <AppUpdateChip state={update} /> : null}
          <Link
            aria-label={t("app.home")}
            className="grid h-6 w-6 place-items-center rounded-full border border-transparent text-[var(--color-on-island)]"
            onClick={(event) => {
              if (isPlainPrimaryClick(event)) {
                event.preventDefault();
                void navigation.openHome();
              }
            }}
            search={{}}
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
          <AppChromeGlobalSearch macOS={macOS} position="trailing" />
          {action}
          <WindowChromeThemeToggle />
        </>
      }
    >
      <TaskSearchHost />
      <ProjectDeletionEventHandler />
      <MainWindowNavigation />
      <AttentionController />
      <div className="min-h-0 min-w-0 flex-1 overflow-visible" data-testid="app-main-content">
        {children}
      </div>
      <SidebarHost />
    </WindowChromeFrame>
  );
}

function AppChromeGlobalSearch({
  macOS,
  position,
}: Readonly<{
  macOS: boolean;
  position: "leading" | "trailing";
}>) {
  const visible = position === "leading" ? !macOS : macOS;
  return visible ? <TaskSearchGlobalTrigger /> : null;
}

function AppChromeFloatingUpdateChip({
  state,
  visible,
}: Readonly<{ state: DesktopUpdateState; visible: boolean }>) {
  return visible ? (
    <div
      className="app-region-no-drag fixed top-[8px] right-[var(--space-4)] z-30 flex h-[22px] items-center"
      data-testid="app-chrome-update-slot"
    >
      <AppUpdateChip state={state} />
    </div>
  ) : null;
}

function ProjectDeletionEventHandler() {
  const { t } = useTranslation();
  const location = useLocation();
  const queryClient = useQueryClient();
  const { nativeBridge } = useAppServices();
  const navigation = useAppNavigation();
  const { push } = useStatusController();
  useProjectDeletedEvents(
    nativeBridge,
    useCallback(
      async (event) => {
        const routeMatches = routeReferencesProject(
          location.pathname,
          new URLSearchParams(location.searchStr).get("projectId"),
          event.projectID,
        );
        return completeProjectDeletion({
          navigateHome: routeMatches ? navigation.openHome : undefined,
          projectID: event.projectID,
          pushDeletedToast: () => {
            push({
              id: "project-delete-deleted",
              tone: "success",
              title: t("projectEdit.deleteDeleted"),
            });
          },
          queryClient,
        });
      },
      [location.pathname, location.searchStr, navigation.openHome, push, queryClient, t],
    ),
  );
  return null;
}

function routeReferencesProject(
  pathname: string,
  selectedHomeProjectID: string | null,
  projectID: string,
): boolean {
  if (pathname === "/" && selectedHomeProjectID === projectID) {
    return true;
  }
  const segments = pathname.split("/").filter((segment) => segment.length > 0);
  return segments[0] === "projects" && segments[1] === projectID;
}

function isPlainPrimaryClick(event: MouseEvent): boolean {
  return event.button === 0 && !event.altKey && !event.ctrlKey && !event.metaKey && !event.shiftKey;
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
