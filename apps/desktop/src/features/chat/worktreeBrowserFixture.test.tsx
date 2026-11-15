import { vi } from "bun:test";
import { useMemo, useState, type ReactNode } from "react";
import { RegistryProvider } from "@effect/atom-react";
import {
  SidebarRootContext,
  SidebarRootOwner,
  SidebarHeaderActionProvider,
  SidebarHeaderActionSlot,
} from "@/app-facade";
import { TestAppProviders, type TestAppServices } from "@/test-support/app-services";
import * as Atom from "effect/reactivity/Atom";
import {
  createTestSidebarNavigator,
  createTestWorktreeBinding,
  createTestSidebarController,
} from "@/test-support/sidebar";
import { WorktreeBrowser as Browser } from "./WorktreeBrowser";

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
