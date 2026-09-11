import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import type { ChatGoalSetResult } from "@/api";
import { TransportError } from "@/api";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import * as ui from "@/ui";
import { NewChatGoalBinding } from "./goalBinding";
import { GoalSidebarPage } from "./GoalSidebar";

const sessionID = "123e4567-e89b-42d3-a456-426614174000";
const target = {
  projectID: "project-1",
  workspace: { workspaceID: "workspace-1" },
  sessionID,
} as const;
const activeGoal = {
  id: "goal-1",
  objective: "Ship the Goal sidebar",
  status: "active",
  created_at: "2026-09-11T10:00:00Z",
  updated_at: "2026-09-11T10:00:00Z",
};

afterEach(() => {
  vi.restoreAllMocks();
});

function mountGoal() {
  const services = createTestServices([
    {
      method: "runtime.goal.pause",
      result: {
        result: {
          kind: "authoritative_goal",
          goal: { ...activeGoal, status: "paused" },
          availability: null,
        },
      },
    },
  ]);
  render(
    <TestAppProviders services={services}>
      <GoalSidebarPage input={{ kind: "session", api: services.api.chat, target }} />
    </TestAppProviders>,
  );
  return services;
}

describe("Goal sidebar", () => {
  it("starts with Loading and then presents authoritative Goal metadata", async () => {
    const testServices = mountGoal();
    expect(screen.getByTestId("loading-state-placeholder")).toBeInTheDocument();

    act(() => {
      testServices.transport.emit("goal.observation", {
        observation: {
          sequence: 1,
          kind: "hydration",
          status: { goal: activeGoal, availability: "available" },
        },
      });
    });

    expect(await screen.findByText("Ship the Goal sidebar")).toBeInTheDocument();
    expect(screen.getByText("Active")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Pause" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Clear" })).toBeInTheDocument();
  });

  it("routes lifecycle actions through the destination controller and updates state", async () => {
    const testServices = mountGoal();
    act(() => {
      testServices.transport.emit("goal.observation", {
        observation: {
          sequence: 1,
          kind: "hydration",
          status: { goal: activeGoal, availability: "available" },
        },
      });
    });
    expect(await screen.findByRole("button", { name: "Pause" })).toBeInTheDocument();

    await userEvent.setup().click(screen.getByRole("button", { name: "Pause" }));

    expect(await screen.findByRole("button", { name: "Resume" })).toBeInTheDocument();
    expect(screen.getByText("Paused")).toBeInTheDocument();
  });

  it("shows a compact retry state when Goal observation fails", async () => {
    const testServices = mountGoal();
    await screen.findByTestId("loading-state");
    await waitFor(() => {
      expect(testServices.transport.subscriptions).toHaveLength(1);
    });
    act(() => {
      testServices.transport.fail("goal.observe", new TransportError("Goal observation failed."));
    });

    await waitFor(() => {
      expect(testServices.transport.subscriptions).toHaveLength(0);
    });
    expect(await screen.findByRole("button", { name: "Try again" })).toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole("button", { name: "Try again" }));

    act(() => {
      testServices.transport.emit("goal.observation", {
        observation: {
          sequence: 1,
          kind: "hydration",
          status: { goal: activeGoal, availability: "available" },
        },
      });
    });
    expect(await screen.findByText("Ship the Goal sidebar")).toBeInTheDocument();
  });

  it("keeps a dirty New Chat draft after a Session-bearing Goal rejection", async () => {
    const testServices = createTestServices([]);
    const result: ChatGoalSetResult = {
      sessionID,
      outcome: { kind: "rejected", error: { kind: "runtime_unavailable" } },
      diagnostic: null,
    };
    const binding = new NewChatGoalBinding({
      api: { setGoal: vi.fn(async () => result) },
      captureTarget: () => ({
        kind: "new_chat",
        projectID: "project-1",
        workspaceID: "workspace-1",
        initialSettings: {
          agentRole: "default",
          supervisor: "off",
          thinking: null,
          fast: null,
          questionsEnabled: true,
          autoCompactionEnabled: true,
        },
      }),
    });
    render(
      <TestAppProviders services={testServices}>
        <GoalSidebarPage input={{ kind: "new_chat", api: testServices.api.chat, binding }} />
      </TestAppProviders>,
    );

    const objective = "Preserve this rejected draft";
    const user = userEvent.setup();
    await user.type(screen.getByRole("textbox", { name: "Goal" }), objective);
    await user.click(screen.getByTestId("goal-save"));

    await screen.findByTestId("loading-state");
    act(() => {
      testServices.transport.emit("goal.observation", {
        observation: {
          sequence: 1,
          kind: "hydration",
          status: { goal: null, availability: "available" },
        },
      });
    });

    expect(await screen.findByDisplayValue(objective)).toBeInTheDocument();
    expect(screen.getByTestId("goal-save")).toBeInTheDocument();
  });

  it("notifies a New Chat Goal failure after the sidebar closes", async () => {
    const services = createTestServices([]);
    const pending = deferred<ChatGoalSetResult>();
    const notice = vi.spyOn(ui, "showStatusToast").mockImplementation(() => undefined);
    const binding = new NewChatGoalBinding({
      api: { setGoal: vi.fn(async () => pending.promise) },
      captureTarget: () => ({
        kind: "new_chat",
        projectID: "project-1",
        workspaceID: "workspace-1",
        initialSettings: {
          agentRole: "default",
          supervisor: "off",
          thinking: null,
          fast: null,
          questionsEnabled: true,
          autoCompactionEnabled: true,
        },
      }),
    });
    const view = render(
      <TestAppProviders services={services}>
        <GoalSidebarPage input={{ kind: "new_chat", api: services.api.chat, binding }} />
      </TestAppProviders>,
    );
    const user = userEvent.setup();
    await user.type(screen.getByRole("textbox", { name: "Goal" }), "close-safe failure");
    await user.click(screen.getByTestId("goal-save"));
    view.unmount();

    await act(async () => {
      pending.reject(new TransportError("connection loss"));
      await pending.promise.catch(() => undefined);
    });

    expect(notice).toHaveBeenCalledOnce();
    expect(notice.mock.lastCall?.[0].tone).toBe("danger");
  });
});

function deferred<Value>() {
  let resolve!: (value: Value) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<Value>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}
