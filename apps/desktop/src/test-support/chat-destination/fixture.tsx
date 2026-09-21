import { render } from "@testing-library/react";
import type { ReactNode } from "react";
import { QueryClient } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterContextProvider,
} from "@tanstack/react-router";
import type { ChatSettingsRead, InitialChatSettings } from "@/api";
import { createTestServices, TestAppProviders, type TestAppServices } from "@/test-support/app-services";
import { deferred, mainViewRead, target as sessionTarget, transcriptPage } from "@/test-support/chat-runtime";
import { worktreeBrowserFixtureRoute } from "@/test-support/api";
import {
  ChatPromptPresenceProvider,
  SidebarRootContext,
  SidebarRootOwner,
  SidebarShellContext,
} from "@/app-facade";
import { createTestSidebarController, createTestSidebarShell } from "@/test-support/sidebar";
import { ChatDestination, type ChatDestinationOpening } from "@/features/chat";
import type { ChatTranscriptHandler, PromptAnswerBatchResponse } from "@/api";

const opening = {
  kind: "new_chat",
  projectID: "project-1",
  workspace: { id: "workspace-1", name: "Default", rootPath: "/default", isDefault: true },
} as const;
const newChatTarget = {
  kind: "new_chat",
  projectID: opening.projectID,
  workspace: { workspaceID: opening.workspace.id },
} as const;
const baseline: InitialChatSettings = {
  agentRole: "default",
  supervisor: "edits",
  thinking: null,
  fast: null,
  questionsEnabled: true,
  autoCompactionEnabled: true,
};
const catalog: Extract<ChatSettingsRead, { kind: "new_chat" }> = {
  kind: "new_chat",
  initialSettings: baseline,
  catalog: {
    choices: [
      {
        agent: {
          role: "default",
          model: "local",
          thinking: "none",
          tools: [],
          customSystemPrompt: false,
          customCapabilities: false,
          agentCallable: true,
        },
        baseline,
        supervisor: { value: "edits", baseline: "edits", editability: { kind: "editable" } },
        thinking: { kind: "unsupported" },
        fast: { kind: "unsupported" },
        questions: { capable: true, enabled: true, editability: { kind: "editable" } },
        autoCompaction: {
          policy: "optional",
          stored: true,
          effective: true,
          editability: { kind: "editable" },
        },
      },
    ],
  },
};
const navigation = {
  openTask: vi.fn<(taskID: string) => void>(),
  openParentSession: vi.fn<(sessionID: string) => void | Promise<void>>(),
};
function renderDestination(
  services: TestAppServices,
  client: QueryClient,
  opening: ChatDestinationOpening,
  contextualContent?: ReactNode,
) {
  const root = createRootRoute();
  const session = createRoute({
    getParentRoute: () => root,
    path: "/projects/$projectId/sessions/$sessionId",
  });
  const router = createRouter({ history: createMemoryHistory(), routeTree: root.addChildren([session]) });
  const view = render(
    <RouterContextProvider router={router}>
      <TestAppProviders services={services} queryClient={client}>
        <SidebarRootContext.Provider value={createTestSidebarController()}>
          <SidebarShellContext.Provider value={createTestSidebarShell()}>
            <SidebarRootOwner>
              <ChatPromptPresenceProvider>
                <ChatDestination opening={opening} navigation={navigation} />
                {contextualContent}
              </ChatPromptPresenceProvider>
            </SidebarRootOwner>
          </SidebarShellContext.Provider>
        </SidebarRootContext.Provider>
      </TestAppProviders>
    </RouterContextProvider>,
  );
  return { unmount: view.unmount, router };
}

function sessionWithPrompts(
  configure?: (services: TestAppServices) => void,
  selected: ChatDestinationOpening = { kind: "session", ...sessionTarget },
  contextualContent?: ReactNode,
) {
  const services = createTestServices([worktreeBrowserFixtureRoute()]);
  const choice = catalog.catalog.choices[0];
  if (choice === undefined) throw new Error("Missing settings choice");
  const settings = vi.spyOn(services.api.chat, "getSettings").mockResolvedValue({
    kind: "session",
    session: { sessionID: sessionTarget.sessionID, previousSessionID: null, task: null },
    settings: {
      selectedAgent: { role: choice.agent.role, model: choice.agent.model, thinking: choice.agent.thinking },
      agentChoices: [choice.agent],
      agentEditability: { kind: "editable" },
      agentLocked: false,
      cachingLocked: false,
      workflowLocked: false,
      supervisor: choice.supervisor,
      thinking: choice.thinking,
      fast: choice.fast,
      questions: choice.questions,
      autoCompaction: choice.autoCompaction,
    },
  });
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({ input: "", protectedInput: null });
  vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
  vi.spyOn(services.api.chat, "getMainView").mockResolvedValue(mainViewRead());
  vi.spyOn(services.api.chat, "getTranscriptPage").mockResolvedValue(transcriptPage(null));
  const pending = vi.spyOn(services.api.chat, "listPendingWork").mockResolvedValue({ items: [] });
  const handlers: ChatTranscriptHandler[] = [];
  vi.spyOn(services.api.chat, "subscribeTranscript").mockImplementation((_target, handler) => {
    handlers.push(handler);
    return { close: vi.fn() };
  });
  const response = deferred<PromptAnswerBatchResponse>();
  const answer = vi.spyOn(services.api.chat, "answerPromptBatch").mockReturnValue(response.promise);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  configure?.(services);
  const view = renderDestination(services, client, selected, contextualContent);
  return { ...view, services, handlers, settings, pending, answer, client, response };
}

export { opening, newChatTarget, baseline, catalog, navigation, renderDestination, sessionWithPrompts };
