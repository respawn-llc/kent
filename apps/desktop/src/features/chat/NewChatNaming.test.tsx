import { afterEach, beforeEach, expect, it, vi } from "bun:test";
import { act, waitFor } from "@testing-library/react";
import { baseline, newChatTarget } from "@/test-support/chat-destination";
import { deferred } from "@/test-support/chat-runtime";
import { installChatStorageFixture } from "./chatStorageFixture";
import { setup } from "./chatDestinationFixture.test";

let restoreStorage: (() => void) | undefined;
beforeEach(() => {
  restoreStorage = installChatStorageFixture();
});
afterEach(() => {
  restoreStorage?.();
  restoreStorage = undefined;
});

it("sets and clears a temporary name without creating a Session", async () => {
  const view = setup();
  const setName = vi.spyOn(view.services.api.chat, "setSessionName");
  await waitFor(() => {
    expect(view.result.current.settings.kind).toBe("ready-new-chat");
  });
  act(() => {
    view.result.current.composer.edit("/name Later");
  });
  await act(async () => {
    view.result.current.composer.submit("send");
  });
  expect(view.result.current.temporaryName).toBe("Later");
  expect(view.result.current.composer.text).toBe("");
  act(() => {
    view.result.current.composer.edit("/name");
  });
  await act(async () => {
    view.result.current.composer.submit("send");
  });
  expect(view.result.current.temporaryName).toBeNull();
  expect(view.settingsRead).toHaveBeenCalledTimes(1);
  expect(view.commandsRead).toHaveBeenCalledTimes(1);
  expect(view.result.current.target?.kind).toBe("new_chat");
  expect(setName).not.toHaveBeenCalled();
  expect(view.steer).not.toHaveBeenCalled();
  expect(view.queue).not.toHaveBeenCalled();
});

it.each(["send", "queue", "compact"] as const)(
  "transfers a temporary name and settings through %s",
  async (action) => {
    const view = setup();
    await waitFor(() => {
      expect(view.result.current.settings.kind).toBe("ready-new-chat");
    });
    act(() => {
      const settings = view.result.current.settings;
      if (settings.kind !== "ready-new-chat") throw new Error("Settings unavailable");
      settings.activate({ kind: "supervisor", value: "off" });
    });
    act(() => {
      view.result.current.composer.edit("/name   Manager  ");
    });
    await act(async () => {
      view.result.current.composer.submit("send");
    });
    act(() => {
      view.result.current.composer.edit("/wt switch feature");
    });
    await act(async () => {
      view.result.current.composer.submit("send");
    });
    const mutation = action === "compact" ? view.compact : action === "queue" ? view.queue : view.steer;
    mutation.mockReturnValue(deferred<never>().promise);
    act(() => {
      view.result.current.composer.edit("first input");
    });
    await act(async () => {
      if (action === "compact") view.result.current.composer.compact();
      else view.result.current.composer.submit(action);
    });
    await waitFor(() => {
      expect(mutation).toHaveBeenCalled();
    });
    expect(mutation.mock.calls[0]?.[0]).toEqual({
      ...newChatTarget,
      initialSettings: { ...baseline, supervisor: "off" },
      name: "Manager",
      worktreeSelector: "feature",
    });
  },
);
