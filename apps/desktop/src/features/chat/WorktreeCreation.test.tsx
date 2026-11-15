import { expect, it, vi } from "bun:test";
import { act, renderHook, waitFor } from "@testing-library/react";
import { RegistryProvider, useAtomValue } from "@effect/atom-react";
import * as Atom from "effect/reactivity/Atom";
import { QueryClient } from "@tanstack/react-query";
import { createTestServices } from "@/test-support/app-services";
import { createTestSidebarNavigator } from "@/test-support/sidebar";
import { target } from "@/test-support/chat-runtime";
import { appI18n } from "@/i18n";
import { createWorktreeCreate, useWorktreeCreate } from "./WorktreeCreate";
import { resolvedCreateTarget } from "./worktreeCreateTestFixture";

it.each(["plan", "worktree"] as const)(
  "retains the delivered identity boundary on %s failure",
  async (failure) => {
    const services = createTestServices([]);
    const resolve = vi
      .spyOn(services.api, "resolveWorktreeCreateTarget")
      .mockResolvedValue(resolvedCreateTarget);
    const planned = vi.spyOn(services.api.chat, "planNewChat").mockResolvedValue(target.sessionID);
    if (failure === "plan") planned.mockRejectedValue(new Error("plan failed"));
    const create = vi.spyOn(services.api, "createWorktree").mockRejectedValue(new Error("setup failed"));
    const delivered = vi.fn(async () => undefined);
    const creationTarget = {
      kind: "new_chat",
      projectID: target.projectID,
      workspace: { workspaceID: "workspace-1" },
      name: "Manager",
      worktreeSelector: "previous",
      initialSettings: {
        agentRole: "default",
        supervisor: "off",
        thinking: null,
        fast: null,
        questionsEnabled: true,
        autoCompactionEnabled: true,
      },
    } as const;
    const scope = { kind: "workspace", projectID: target.projectID, workspaceID: "workspace-1" } as const;
    const model = createWorktreeCreate({
      client: new QueryClient(),
      api: services.api,
      logger: services.logger,
      projectID: target.projectID,
      chat: {
        scope: Atom.make(scope),
        selector: Atom.make("previous"),
        workspaceName: Atom.make("Workspace"),
        creationTarget: Atom.make(creationTarget),
        draft: Atom.make("unsent"),
        creationKey: ["first-worktree"],
        select: vi.fn(),
        delivered,
      },
      openSession: vi.fn(),
      suggestion: "topic",
      navigator: createTestSidebarNavigator(),
      refreshOpenWorktreeList: vi.fn(),
      submitSwitch: vi.fn(),
      push: vi.fn(),
      t: appI18n.t,
    });
    const { result } = renderHook(
      () => ({ actions: useWorktreeCreate(model), state: useAtomValue(model.state) }),
      { wrapper: RegistryProvider },
    );
    await waitFor(() => {
      expect(result.current.state.resolving).toBe(false);
    });
    expect(resolve).toHaveBeenCalledWith(scope, "topic");
    expect(planned).not.toHaveBeenCalled();
    await act(async () => {
      result.current.actions.submit(undefined);
    });
    await waitFor(() => {
      expect(planned).toHaveBeenCalled();
    });
    await waitFor(() => {
      expect(result.current.state.pending).toBe(false);
    });
    expect(planned).toHaveBeenCalledWith(creationTarget, "unsent");
    if (failure === "plan") {
      expect(delivered).not.toHaveBeenCalled();
      expect(create).not.toHaveBeenCalled();
    } else {
      expect(delivered).toHaveBeenCalledWith(target.sessionID);
      expect(create.mock.calls[0]?.[0].sessionID).toBe(target.sessionID);
    }
  },
);
