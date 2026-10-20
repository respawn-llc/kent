import { afterEach, expect, it, vi } from "bun:test";
import { act, renderHook, waitFor } from "@testing-library/react";
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import * as Atom from "effect/reactivity/Atom";
import { useAtomSet } from "@effect/atom-react";
import {
  pendingWorkItemIDFromProto,
  type ChatInputMutationResult,
  type ChatRuntimeActivity,
  type PendingWorkItemID,
} from "@/api";
import { appI18n } from "@/i18n";
import { createTestServices } from "@/test-support/app-services";
import { composerWrapper } from "@/test-support/composer";
import { createChatComposerViewModel } from "./ChatComposerViewModel";
import { useChatComposer } from "./useChatComposer";
import { target } from "./chatComposerTestFixture";
import * as ui from "@/ui";

const idle: ChatRuntimeActivity = {
  state: "registered_idle",
  activeStep: null,
  reviewer: "inactive",
  queueAccepting: true,
  diagnosticRecovery: false,
};
async function setup(intent: "send" | "queue" = "send", activity?: ChatRuntimeActivity) {
  const services = createTestServices([]);
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({ input: "original", protectedInput: null });
  vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
  vi.spyOn(ui, "showStatusToast").mockImplementation(() => undefined);
  let accept!: (result: ChatInputMutationResult) => void;
  let fail!: (error: Error) => void;
  const request = new Promise<ChatInputMutationResult>((resolve, reject) => {
    accept = resolve;
    fail = reject;
  });
  const send = vi.spyOn(services.api.chat, intent === "send" ? "steer" : "queue").mockReturnValue(request);
  const rejected = vi.fn<(id: PendingWorkItemID) => void>();
  const transferPending = Atom.make(false);
  const { result, unmount } = renderHook(
    () => {
      const client = useQueryClient();
      const [model] = useState(() =>
        createChatComposerViewModel({
          services,
          client,
          target: Atom.make(target),
          opening: target,
          submission: Atom.make({ kind: "ready" } as const),
          t: appI18n.t,
          transferPending,
        }),
      );
      return { ...useChatComposer({ model, rejected }), setTransferPending: useAtomSet(transferPending) };
    },
    { wrapper: composerWrapper(services) },
  );
  await waitFor(() => {
    expect(result.current.draft.kind).toBe("ready");
  });
  vi.useFakeTimers();
  await act(async () => {
    result.current.submit(intent, "editor", activity);
  });
  const inputID = send.mock.calls[0]?.[2];
  if (inputID === undefined) throw new Error("Expected one submitted input.");
  return { result, inputID, accept, fail, send, rejected, unmount };
}

afterEach(() => vi.useRealTimers());

it("holds unknown-activity source readonly, then unlocks after two seconds without resending", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({ input: "original", protectedInput: null });
  vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
  const steer = vi
    .spyOn(services.api.chat, "steer")
    .mockImplementation(async () => new Promise<ChatInputMutationResult>(() => undefined));
  const { result } = renderHook(
    () => {
      const client = useQueryClient();
      const [model] = useState(() =>
        createChatComposerViewModel({
          services,
          client,
          target: Atom.make(target),
          opening: target,
          submission: Atom.make({ kind: "ready" } as const),
          t: appI18n.t,
        }),
      );
      return useChatComposer({ model });
    },
    { wrapper: composerWrapper(services) },
  );
  await waitFor(() => {
    expect(result.current.draft.kind).toBe("ready");
  });
  vi.useFakeTimers();
  await act(async () => {
    result.current.submit("send");
  });
  expect(result.current.text).toBe("original");
  expect(result.current.interactionRestricted).toBe(true);
  await act(async () => {
    result.current.edit("replacement");
  });
  expect(result.current.text).toBe("original");
  await act(async () => {
    vi.advanceTimersByTime(1_999);
  });
  expect(result.current.text).toBe("original");
  await act(async () => {
    vi.advanceTimersByTime(1);
  });
  expect(result.current.text).toBe("");
  expect(result.current.interactionRestricted).toBe(false);
  expect(result.current.outgoing).toHaveLength(1);
  expect(result.current.outgoing[0]).toMatchObject({
    text: "original",
    placement: "pending",
    confirmation: "awaiting",
  });
  await act(async () => {
    result.current.edit("newer");
  });
  expect(result.current.text).toBe("newer");
  expect(steer).toHaveBeenCalledTimes(1);
});

it.each(["accepted", "not_accepted", "error"] as const)(
  "committed authority wins over late %s and stale Pending Work",
  async (outcome) => {
    const { result, inputID, accept, fail, rejected } = await setup();
    const pendingItem = {
      id: pendingWorkItemIDFromProto(inputID.toJSONValue()),
      kind: "message" as const,
      lane: "steer" as const,
      state: "pending" as const,
      canonicalInput: "original",
      message: { text: "original" },
    };
    await act(async () => {
      result.current.observeOutgoing({
        committedIDs: [inputID, pendingWorkItemIDFromProto(inputID.toJSONValue())],
        pendingItems: [pendingItem],
        pendingFetching: true,
      });
    });
    expect(result.current.outgoing).toEqual([]);
    expect(result.current.pending.items).toEqual([]);
    expect(result.current.text).toBe("");
    await act(async () => {
      result.current.edit("newer");
    });
    await act(async () => {
      if (outcome === "error") fail(new Error("lost response"));
      else
        accept({
          sessionID: target.sessionID,
          outcome:
            outcome === "accepted"
              ? { kind: "accepted", queueItemID: inputID, disposition: "pending_steer", diagnostic: null }
              : { kind: "not_accepted", reason: { kind: "runtime_unavailable" } },
        });
    });
    expect(result.current.text).toBe("newer");
    expect(result.current.outgoing).toEqual([]);
    expect(result.current.pending.items).toEqual([]);
    expect(rejected).not.toHaveBeenCalled();
    await act(async () => {
      result.current.observeOutgoing({
        committedIDs: [],
        pendingItems: [pendingItem],
        pendingFetching: false,
      });
    });
    expect(result.current.pending.items).toEqual([]);
  },
);

it("reconciles canonical committed IDs from the stable runtime observation without a resident row", async () => {
  const { result, inputID, fail, rejected } = await setup();
  const observation = result.current.observation;
  await act(async () => {
    observation.onUserInputCommitted([
      inputID.toJSONValue(),
      inputID.toJSONValue(),
      pendingWorkItemIDFromProto("5b26bfad-a049-49a8-a4cd-cdc2bbf6ea36").toJSONValue(),
    ]);
  });
  expect(result.current.text).toBe("");
  expect(result.current.outgoing).toEqual([]);
  expect(result.current.observation).toBe(observation);
  await act(async () => {
    result.current.edit("newer");
  });
  await act(async () => {
    fail(new Error("lost response"));
  });
  expect(result.current.text).toBe("newer");
  expect(rejected).not.toHaveBeenCalled();
});

it.each(["send", "queue"] as const)(
  "%s uses its own late disposition and preserves newer typing",
  async (intent) => {
    const { result, inputID, accept } = await setup(intent, idle);
    expect(result.current.text).toBe(intent === "send" ? "" : "original");
    expect(result.current.heldReadonly).toBe(intent === "queue");
    expect(result.current.outgoing[0]?.placement).toBe(intent === "send" ? "bubble" : "held");
    await act(async () => {
      vi.advanceTimersByTime(2_000);
    });
    await act(async () => {
      result.current.edit("newer");
    });
    await act(async () => {
      accept({
        sessionID: target.sessionID,
        outcome: { kind: "accepted", queueItemID: inputID, disposition: "started_turn", diagnostic: null },
      });
    });
    expect(result.current.text).toBe("newer");
    expect(result.current.outgoing).toHaveLength(1);
    expect(result.current.outgoing[0]).toMatchObject({
      confirmation: "accepted",
      placement: intent === "send" ? "bubble" : "pending",
    });
  },
);

it.each(["started_turn", "pending_steer", "pending_queue"] as const)(
  "acceptance before the deadline clears held text and presents %s",
  async (disposition) => {
    const { result, inputID, accept } = await setup();
    await act(async () => {
      accept({
        sessionID: target.sessionID,
        outcome: { kind: "accepted", queueItemID: inputID, disposition, diagnostic: null },
      });
    });
    expect(result.current.text).toBe("");
    expect(result.current.heldReadonly).toBe(false);
    expect(result.current.outgoing[0]).toMatchObject({
      placement: disposition === "started_turn" ? "bubble" : "pending",
      confirmation: "accepted",
      disposition,
    });
    await act(async () => {
      result.current.edit("newer");
    });
    await act(async () => {
      vi.advanceTimersByTime(2_000);
    });
    expect(result.current.text).toBe("newer");
  },
);

it("accepted held-input cleanup completes while navigation restricts ordinary editing", async () => {
  const { result, inputID, accept } = await setup();
  await act(async () => {
    result.current.setTransferPending(true);
  });
  expect(result.current.interactionRestricted).toBe(true);
  await act(async () => {
    accept({
      sessionID: target.sessionID,
      outcome: { kind: "accepted", queueItemID: inputID, disposition: "started_turn", diagnostic: null },
    });
  });
  expect(result.current.text).toBe("");
  expect(result.current.heldReadonly).toBe(false);
  expect(result.current.interactionRestricted).toBe(true);
});

it("bounds unresolved presentation admission and preserves the unsent draft at capacity", async () => {
  const { result, send, inputID, accept } = await setup("send", idle);
  for (let index = 1; index < 100; index++) {
    await act(async () => {
      result.current.edit("original");
      result.current.submit("send", "editor", idle);
    });
  }
  expect(result.current.outgoing).toHaveLength(100);
  expect(new Set(result.current.outgoing.map((input) => input.inputID.toJSONValue())).size).toBe(100);
  expect(result.current.canPrepare).toBe(false);
  await act(async () => {
    result.current.edit("unsent");
    result.current.submit("send", "editor", idle);
  });
  expect(result.current.text).toBe("unsent");
  expect(send).toHaveBeenCalledTimes(100);
  await act(async () => {
    result.current.observeOutgoing({
      committedIDs: result.current.outgoing.map((input) => input.inputID),
      pendingItems: [],
      pendingFetching: true,
    });
  });
  expect(result.current.outgoing).toEqual([]);
  expect(result.current.canPrepare).toBe(false);
  await act(async () => {
    accept({
      sessionID: target.sessionID,
      outcome: { kind: "accepted", queueItemID: inputID, disposition: "started_turn", diagnostic: null },
    });
  });
  expect(result.current.canPrepare).toBe(false);
  await act(async () => {
    result.current.observeOutgoing({
      committedIDs: [],
      pendingItems: [],
      pendingFetching: false,
    });
  });
  expect(result.current.canPrepare).toBe(true);
  expect(result.current.text).toBe("unsent");
});

it.each(["accepted", "not_accepted", "error"] as const)(
  "an older %s cannot consume a newer held submission",
  async (outcome) => {
    const { result, send, inputID, accept, fail } = await setup();
    await act(async () => {
      vi.advanceTimersByTime(2_000);
    });
    let acceptSecond!: (result: ChatInputMutationResult) => void;
    send.mockImplementationOnce(
      async () =>
        new Promise<ChatInputMutationResult>((resolve) => {
          acceptSecond = resolve;
        }),
    );
    await act(async () => {
      result.current.edit("second");
      result.current.submit("send");
    });
    const secondID = send.mock.calls[1]?.[2];
    if (secondID === undefined) throw new Error("Expected second submitted input.");
    await act(async () => {
      if (outcome === "error") fail(new Error("lost response"));
      else
        accept({
          sessionID: target.sessionID,
          outcome:
            outcome === "accepted"
              ? { kind: "accepted", queueItemID: inputID, disposition: "started_turn", diagnostic: null }
              : { kind: "not_accepted", reason: { kind: "runtime_unavailable" } },
        });
    });
    expect(result.current.text).toBe("second");
    expect(result.current.heldReadonly).toBe(true);
    await act(async () => {
      acceptSecond({
        sessionID: target.sessionID,
        outcome: { kind: "accepted", queueItemID: secondID, disposition: "started_turn", diagnostic: null },
      });
    });
    expect(result.current.text).toBe(outcome === "accepted" ? "" : "original");
    expect(result.current.heldReadonly).toBe(false);
    expect(send).toHaveBeenCalledTimes(2);
  },
);

it.each(["not_accepted", "error"] as const)(
  "late %s appends the original once without replacing newer typing",
  async (outcome) => {
    const { result, inputID, accept, fail, rejected } = await setup();
    await act(async () => {
      vi.advanceTimersByTime(2_000);
    });
    await act(async () => {
      result.current.edit("newer");
    });
    await act(async () => {
      if (outcome === "error") fail(new Error("transport unavailable"));
      else
        accept({
          sessionID: target.sessionID,
          outcome: { kind: "not_accepted", reason: { kind: "runtime_unavailable" } },
        });
    });
    expect(result.current.text).toBe("newer\noriginal");
    expect(result.current.outgoing).toEqual([]);
    expect(rejected).toHaveBeenCalledTimes(1);
    expect(rejected).toHaveBeenCalledWith(inputID);
    await act(async () => {
      vi.advanceTimersByTime(2_000);
    });
    expect(result.current.text).toBe("newer\noriginal");
  },
);

it("releases capacity after accepted Pending Work is removed by its authority", async () => {
  const { result, inputID, accept } = await setup("queue");
  const pendingItem = {
    id: inputID,
    kind: "message" as const,
    lane: "queue" as const,
    state: "pending" as const,
    canonicalInput: "original",
    message: { text: "original" },
  };
  await act(async () => {
    result.current.observeOutgoing({
      committedIDs: [],
      pendingItems: [pendingItem],
      pendingFetching: false,
    });
  });
  await act(async () => {
    accept({
      sessionID: target.sessionID,
      outcome: { kind: "accepted", queueItemID: inputID, disposition: "pending_queue", diagnostic: null },
    });
  });
  await act(async () => {
    result.current.observeOutgoing({
      committedIDs: [],
      pendingItems: [],
      pendingFetching: false,
    });
  });
  expect(result.current.outgoing).toEqual([]);
  expect(result.current.pending.items).toEqual([]);
  expect(result.current.text).toBe("");
});

it.each(["not_accepted", "error"] as const)(
  "held %s leaves the original source untouched",
  async (outcome) => {
    const { result, accept, fail } = await setup("queue");
    await act(async () => {
      if (outcome === "error") fail(new Error("transport unavailable"));
      else
        accept({
          sessionID: target.sessionID,
          outcome: { kind: "not_accepted", reason: { kind: "runtime_unavailable" } },
        });
    });
    expect(result.current.text).toBe("original");
    expect(result.current.heldReadonly).toBe(false);
    expect(result.current.outgoing).toEqual([]);
    await act(async () => {
      result.current.edit("newer");
    });
    await act(async () => {
      vi.advanceTimersByTime(2_000);
    });
    expect(result.current.text).toBe("newer");
  },
);

it.each(["not_accepted", "error"] as const)(
  "authoritative Pending Work releases held source and defeats late %s",
  async (outcome) => {
    const { result, inputID, accept, fail, rejected } = await setup("queue");
    const pendingItem = {
      id: pendingWorkItemIDFromProto(inputID.toJSONValue()),
      kind: "message" as const,
      lane: "queue" as const,
      state: "pending" as const,
      canonicalInput: "original",
      message: { text: "original" },
    };
    await act(async () => {
      result.current.observeOutgoing({
        committedIDs: [],
        pendingItems: [pendingItem],
        pendingFetching: false,
      });
    });
    expect(result.current.text).toBe("");
    expect(result.current.heldReadonly).toBe(false);
    expect(result.current.outgoing).toEqual([]);
    expect(result.current.pending.items).toEqual([pendingItem]);
    await act(async () => {
      result.current.edit("newer");
    });
    await act(async () => {
      if (outcome === "error") fail(new Error("lost response"));
      else
        accept({
          sessionID: target.sessionID,
          outcome: { kind: "not_accepted", reason: { kind: "runtime_unavailable" } },
        });
    });
    expect(result.current.text).toBe("newer");
    expect(rejected).not.toHaveBeenCalled();
  },
);
