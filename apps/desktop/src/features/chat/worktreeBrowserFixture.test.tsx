import { vi } from "bun:test";
import { useMemo, useState, type ReactNode } from "react";
import { RegistryProvider } from "@effect/atom-react";
import {
  SidebarRootContext,
  SidebarRootOwner,
  SidebarHeaderActionProvider,
  SidebarHeaderActionSlot,
} from "@/app-facade";
import { createTestServices, TestAppProviders, type TestAppServices } from "@/test-support/app-services";
import { QueryClient } from "@tanstack/react-query";
import { appI18n } from "@/i18n";
import { createWorktreeCreate } from "./WorktreeCreate";
import * as Atom from "effect/reactivity/Atom";
import {
  createTestSidebarNavigator,
  createTestWorktreeBinding,
  createTestSidebarController,
} from "@/test-support/sidebar";
import { WorktreeBrowser as Browser } from "./WorktreeBrowser";

export function createFormModel() {
  const services = createTestServices([]);
  const push = vi.fn();
  const model = createWorktreeCreate({
    openSession: vi.fn(),
    client: new QueryClient(),
    api: services.api,
    logger: services.logger,
    projectID: "project-1",
    chat: createTestWorktreeBinding({
      kind: "session",
      projectID: "project-1",
      sessionID: "33333333-3333-4333-8333-333333333333",
    }),
    suggestion: "topic",
    navigator: createTestSidebarNavigator(),
    refreshOpenWorktreeList: vi.fn(),
    submitSwitch: vi.fn(),
    push,
    t: appI18n.t,
  });
  return { services, push, model };
}

export function BrowserProviders({
  services,
  children,
}: Readonly<{ services: TestAppServices; children: ReactNode }>) {
  const [roots] = useState(() => createTestSidebarController());
  return (
    <TestAppProviders services={services} skipAnimations>
      <RegistryProvider>
        <SidebarRootContext.Provider value={roots}>
          <SidebarRootOwner>
            <SidebarHeaderActionProvider>
              <SidebarHeaderActionSlot />
              {children}
            </SidebarHeaderActionProvider>
          </SidebarRootOwner>
        </SidebarRootContext.Provider>
      </RegistryProvider>
    </TestAppProviders>
  );
}

export function WorktreeBrowser({
  onOpenSession = vi.fn(),
  workspace = false,
  projectID,
  sessionID,
}: Readonly<{
  projectID: string;
  sessionID: string;
  workspace?: boolean;
  onOpenSession?: (sessionID: string) => void;
}>) {
  const [navigator] = useState(() => createTestSidebarNavigator());
  const chat = useMemo(() => {
    const binding = createTestWorktreeBinding({ kind: "session", projectID, sessionID });
    return workspace
      ? {
          ...binding,
          scope: Atom.make({ kind: "workspace" as const, projectID, workspaceID: "workspace-1" }),
        }
      : binding;
  }, [projectID, sessionID, workspace]);
  return (
    <Browser
      projectID={projectID}
      chat={chat}
      onOpenSession={onOpenSession}
      navigator={navigator}
      onCreate={vi.fn()}
      onSwitch={vi.fn()}
      refreshOpenWorktreeList={vi.fn()}
    />
  );
}
