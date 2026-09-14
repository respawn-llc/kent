import { useCallback, useMemo, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import {
  ChatRuntimeProvider,
  createRefreshOpenWorktreeList,
  SidebarRootOwner,
  useAppServices,
  useSidebarShell,
  useStatusController,
  worktreeTransitionOutcomeHandler,
  type ChatRuntimeApi,
} from "@/app-facade";
import { WorktreeControl } from "@/features/chat";
import { target } from "@/dev-showcase/fixtures";
import { SidebarProvider } from "../sidebarProvider";
import { sidebarDestinationPolicy } from "../sidebarDestinationPolicy";
import { useSidebarCurrentPage } from "../sidebarPageContext";
import { SidebarHost } from "../sidebar";

export function WorktreeFixtureSurface({
  api,
  children,
}: Readonly<{ api: ChatRuntimeApi; children?: ReactNode }>) {
  return (
    <SidebarProvider policy={sidebarDestinationPolicy}>
      <SidebarRootOwner>
        <FixtureChat api={api}>{children}</FixtureChat>
      </SidebarRootOwner>
    </SidebarProvider>
  );
}

function FixtureChat({ api, children }: Readonly<{ api: ChatRuntimeApi; children: ReactNode }>) {
  const services = useAppServices();
  const client = useQueryClient();
  const shell = useSidebarShell();
  const page = useSidebarCurrentPage();
  const { push } = useStatusController();
  const { t } = useTranslation();
  const refresh = useMemo(
    () => createRefreshOpenWorktreeList(client, services.api, shell.currentSurface),
    [client, services.api, shell.currentSurface],
  );
  const host = useMemo(
    () => ({
      logger: services.logger,
      onWorktreeTransitionOutcome: worktreeTransitionOutcomeHandler(refresh, target.sessionID, push, t),
    }),
    [push, refresh, services.logger, t],
  );
  const onReconnected = useCallback(() => {
    const current = shell.currentSurface();
    if (current?.kind !== "worktree" || current.sessionID !== target.sessionID) return;
    if (current.page === "create" && page?.destination === current) {
      page.navigator.replace({ ...current, page: "list" });
    } else {
      refresh(target.sessionID);
    }
  }, [page, refresh, shell]);
  return (
    <ChatRuntimeProvider
      api={{ connection: services.api.connection, chat: api }}
      target={target}
      host={host}
      onReconnected={onReconnected}
    >
      {children}
      <WorktreeControl sessionID={target.sessionID} />
      <SidebarHost />
    </ChatRuntimeProvider>
  );
}
