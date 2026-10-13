import { act, renderHook, waitFor } from "@testing-library/react";
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useAtomValue } from "@effect/atom-react";
import * as Atom from "effect/reactivity/Atom";
import { parsePendingWorkItemID, type PendingWorkItemID } from "@/api";
import { createTestServices } from "@/test-support/app-services";
import { composerWrapper } from "@/test-support/composer";
import { appI18n } from "@/i18n";
import { createChatComposerViewModel } from "./ChatComposerViewModel";
import { useComposerInputActions } from "./ComposerInputViewModel";
import { useComposerDraftActions, type ComposerTextRestoration } from "./ComposerDraftViewModel";
import { target } from "./chatComposerTestFixture";
import * as ui from "@/ui";
import type { ComposerCommand } from "./composerCommands";

it.each(["accepted", "not_accepted", "error"] as const)(
  "prepares distinct identities before clearing duplicate input and handles %s",
  async (outcome) => {
    const services = createTestServices([]);
    vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({ input: "same", protectedInput: null });
    vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
    vi.spyOn(ui, "showStatusToast").mockImplementation(() => undefined);
    const steer = vi.spyOn(services.api.chat, "steer").mockImplementation(async (_, __, queueItemID) => {
      if (outcome === "error") throw new Error("transport unavailable");
      return {
        sessionID: target.sessionID,
        outcome:
          outcome === "accepted"
            ? { kind: "accepted", queueItemID, diagnostic: null }
            : { kind: "not_accepted", reason: { kind: "runtime_unavailable" } },
      };
    });
    const { result } = renderHook(
      () => {
        const client = useQueryClient();
        const [model] = useState(() =>
          createChatComposerViewModel({
            services,
            client,
            target: Atom.make(target),
            submission: Atom.make({ kind: "ready" } as const),
            opening: target,
            t: appI18n.t,
          }),
        );
        return {
          input: useComposerInputActions(model.input),
          ...useComposerDraftActions(model.draft),
          text: useAtomValue(model.draft.text),
          read: useAtomValue(model.draft.read),
          request: useAtomValue(model.input.result),
        };
      },
      { wrapper: composerWrapper(services) },
    );
    await waitFor(() => {
      expect(result.current.read.isSuccess).toBe(true);
    });
    const ids: PendingWorkItemID[] = [];
    const prepared = vi.fn((id: PendingWorkItemID) => {
      expect(result.current.text).toBe("same");
      expect(parsePendingWorkItemID(id.toJSONValue()).toJSONValue()).toBe(id.toJSONValue());
      ids.push(id);
    });
    const rejected = vi.fn<(id: PendingWorkItemID) => void>();
    const restore = vi.fn((input: ComposerTextRestoration) => {
      result.current.restore(input);
    });
    const accepted = vi.fn();
    const submit = async (commands: readonly ComposerCommand[] = []) => {
      await act(async () => {
        result.current.input.submit({
          intent: "send",
          source: "editor",
          commands,
          selectedToken: null,
          prepared,
          rejected,
          restore,
          accepted,
          failed: vi.fn(),
        });
      });
      await waitFor(() => {
        expect(result.current.request.isPending).toBe(false);
      });
    };
    await submit();
    await act(async () => {
      result.current.edit("same");
    });
    await submit();
    expect(prepared).toHaveBeenCalledTimes(2);
    expect(new Set(ids.map((id) => id.toJSONValue())).size).toBe(2);
    expect(steer.mock.calls.map((call) => call[2])).toEqual(ids);
    expect(steer.mock.calls.map((call) => call[1])).toEqual([
      { kind: "text", text: "same" },
      { kind: "text", text: "same" },
    ]);
    if (outcome === "accepted") {
      expect(rejected).not.toHaveBeenCalled();
      expect(accepted).toHaveBeenCalledTimes(2);
      expect(result.current.text).toBe("");
    } else {
      expect(rejected.mock.calls.map(([id]) => id)).toEqual(ids);
      expect(restore).toHaveBeenCalledTimes(2);
      expect(result.current.text).toBe("same");
    }
    const local = vi.fn(async () => ({ kind: "local" as const }));
    await act(async () => {
      result.current.edit("/local");
    });
    await submit([
      {
        token: "/local",
        aliases: [],
        description: null,
        preview: null,
        execution: { kind: "direct", send: local },
      },
    ]);
    expect(local).toHaveBeenCalledOnce();
    await act(async () => {
      result.current.edit("/prompt:missing");
    });
    await submit();
    await act(async () => {
      result.current.edit("   ");
    });
    await submit();
    expect(prepared).toHaveBeenCalledTimes(2);
    expect(rejected).toHaveBeenCalledTimes(outcome === "accepted" ? 0 : 2);
    expect(steer).toHaveBeenCalledTimes(2);
  },
);
