import { vi } from "bun:test";
import { renderHook } from "@testing-library/react";
import { RegistryProvider } from "@effect/atom-react";
import type { ReactNode } from "react";
import type { SidebarDestination } from "@/app-facade";
import { writeBrowserStorage, SidebarRootContext, SidebarRootOwner, SidebarShellContext } from "@/app-facade";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { createTestSidebarController, createTestSidebarShell } from "@/test-support/sidebar";
import { opening, catalog, navigation } from "@/test-support/chat-destination";
import { workspaceWorktreeBrowserFixtureRoute } from "@/test-support/api";
import { useChatDestination } from "./useChatDestination";
import type { ChatDestinationOpening } from "./ChatDestinationViewModel";
import type { ComposerCommand } from "./composerCommands";

export function setup(commands: readonly ComposerCommand[] = [], selected: ChatDestinationOpening = opening) {
  writeBrowserStorage("local", "desktop.newChatDraft", "");
  const services = createTestServices([workspaceWorktreeBrowserFixtureRoute()]);
  const commandsRead = vi.spyOn(services.api.chat, "getCommandCatalog").mockResolvedValue([]);
  const settingsRead = vi.spyOn(services.api.chat, "getSettings").mockImplementation(async (target) => {
    if (target.kind === "new_chat") return catalog;
    const choice = catalog.catalog.choices[0];
    if (choice === undefined) throw new Error("Missing default Agent");
    return {
      kind: "session",
      settings: {
        ...choice,
        selectedAgent: choice.agent,
        agentChoices: [choice.agent],
        agentEditability: { kind: "editable" },
        agentLocked: false,
        workflowLocked: false,
        cachingLocked: false,
      },
      session: { sessionID: target.sessionID, shortID: null, previousSessionID: null, task: null },
    };
  });
  const draft = vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
  const read = vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({ input: "", protectedInput: null });
  const observe = vi.spyOn(services.api.chat, "getMainView");
  const steer = vi.spyOn(services.api.chat, "steer");
  const queue = vi.spyOn(services.api.chat, "queue");
  const goal = vi.spyOn(services.api.chat, "setGoal");
  const compact = vi.spyOn(services.api.chat, "compact");
  const opened = vi.fn<(destination: SidebarDestination) => void>();
  const roots = createTestSidebarController(opened);
  vi.spyOn(services.api.chat, "listPendingWork").mockResolvedValue({ items: [] });
  const view = renderHook(() => useChatDestination({ opening: selected, navigation, commands }), {
    wrapper: ({ children }: Readonly<{ children: ReactNode }>) => (
      <RegistryProvider>
        <TestAppProviders services={services}>
          <SidebarRootContext.Provider value={roots}>
            <SidebarShellContext.Provider value={createTestSidebarShell()}>
              <SidebarRootOwner>{children}</SidebarRootOwner>
            </SidebarShellContext.Provider>
          </SidebarRootContext.Provider>
        </TestAppProviders>
      </RegistryProvider>
    ),
  });
  return {
    ...view,
    services,
    draft,
    read,
    observe,
    steer,
    queue,
    goal,
    compact,
    commandsRead,
    settingsRead,
    opened,
  };
}
