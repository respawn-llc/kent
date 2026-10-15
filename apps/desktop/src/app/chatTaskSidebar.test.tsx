import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it, mock, vi } from "bun:test";
import { useState } from "react";
import { RegistryProvider } from "@effect/atom-react";
import type { ChatSettingsOptions } from "@/features/chat";
import { SidebarProvider } from "./sidebarProvider";
import { sidebarDestinationPolicy } from "./sidebarDestinationPolicy";

const openTask = vi.fn();
const actualRouter = await import("@tanstack/react-router");
const actualAppFacade = await import("@/app-facade");
const actualChat = await import("@/features/chat");
const { useSidebarShell } = actualAppFacade;
await mock.module("@tanstack/react-router", () => ({
  ...actualRouter,
  getRouteApi: () => ({
    useParams: () => ({ projectId: "project-1", sessionId: "session-1" }),
  }),
}));
await mock.module("@/app-facade", () => ({
  ...actualAppFacade,
  useAppNavigation: () => ({ openTask }),
  useChatHistoryBookmark: (_projectID: string, sessionID: string | null) => ({
    sessionID,
    delivered: vi.fn(),
  }),
}));
await mock.module("@/features/chat", () => {
  function Chat({ navigation }: Readonly<{ navigation: Pick<ChatSettingsOptions, "openTask"> }>) {
    const [draft, setDraft] = useState("");
    return (
      <>
        <input
          aria-label="draft"
          value={draft}
          onChange={(event) => {
            setDraft(event.target.value);
          }}
        />
        <button
          onClick={() => {
            navigation.openTask("parent-task");
          }}
        >
          parent task
        </button>
      </>
    );
  }
  return { ...actualChat, ChatDestination: Chat };
});

const { ChatRoute, NewChatRoute } = await import("./routeComponents");

function SidebarState() {
  const { activeDestination, close } = useSidebarShell();
  return activeDestination?.kind === "taskDetail" ? (
    <button onClick={close}>{activeDestination.taskID}</button>
  ) : null;
}

it.each([ChatRoute, NewChatRoute])(
  "opens parent Task in the sidebar without leaving chat (%#)",
  async (Route) => {
    openTask.mockClear();
    render(
      <RegistryProvider>
        <SidebarProvider policy={sidebarDestinationPolicy}>
          <Route />
          <SidebarState />
        </SidebarProvider>
      </RegistryProvider>,
    );
    fireEvent.change(screen.getByRole("textbox", { name: "draft" }), { target: { value: "unsent draft" } });
    fireEvent.click(screen.getByRole("button", { name: "parent task" }));
    expect(screen.getByRole("button", { name: "parent-task" })).toBeInTheDocument();
    expect(openTask).not.toHaveBeenCalled();
    expect(screen.getByRole("textbox", { name: "draft" })).toHaveValue("unsent draft");
    fireEvent.click(screen.getByRole("button", { name: "parent-task" }));
    await waitFor(() => {
      expect(screen.queryByRole("button", { name: "parent-task" })).not.toBeInTheDocument();
    });
    expect(screen.getByRole("textbox", { name: "draft" })).toHaveValue("unsent draft");
  },
);
