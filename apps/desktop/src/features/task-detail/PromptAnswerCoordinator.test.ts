import { describe, expect, it, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { queryKeys } from "@/app-facade";
import type { QuestionAttentionItem } from "@/api";
import { parsedQuestionAttention } from "@/test-support/task-detail";
import { PromptAnswerCoordinator } from "./PromptAnswerCoordinator";
import { emptyPromptAnswerState, promptAnswerKey, samePromptAnswerKey } from "./PromptAnswerState";
import { taskDetailAttentionRowKey } from "./TaskDetailAttentionRowKey";
import { emptyQuestionSelection, withQuestionCommentary } from "./TaskDetailQuestionState";

type CoordinatorOptions = ConstructorParameters<typeof PromptAnswerCoordinator>[0];

describe("Task Detail prompt answers", () => {
  it("keeps acknowledged attention absent while success refresh waits and fails", async () => {
    const answered = question("step-1", "prompt-1");
    const sibling = question("step-2", "prompt-1");
    const client = new QueryClient();
    const key = queryKeys.taskAttention("task-1");
    const projection = { items: [answered, sibling], generatedAt: 1 };
    client.setQueryData(key, projection);
    const refresh = deferred<undefined>();
    const harness = coordinatorHarness(
      [
        [answered, draft("answer")],
        [sibling, draft("sibling")],
      ],
      {
        queryClient: client,
        invalidateAttention: async () => refresh.promise,
      },
    );
    const attempt = submit(harness.coordinator, answered, "answer", async () => undefined);
    await vi.waitFor(() => {
      expect(harness.state.isMasked(promptAnswerKey(answered))).toBe(false);
    });
    expect(client.getQueryData(key)).toEqual({ ...projection, items: [sibling] });
    refresh.reject(new Error("refresh failed"));
    await attempt;
    expect(client.getQueryData(key)).toEqual({ ...projection, items: [sibling] });
    expect(harness.state.selection(promptAnswerKey(sibling))?.answer).toBe("sibling");
    client.clear();
  });
  it("restores an ambiguous failed answer without a follow-up read", async () => {
    const attention = question("step-1", "prompt-1");
    const invalidate = vi.fn(async () => undefined);
    const harness = coordinatorHarness([[attention, draft("retry")]], {
      invalidateAttention: invalidate,
    });
    await submit(harness.coordinator, attention, "retry", async () => {
      throw new Error("delivery failed");
    });
    expect(invalidate).not.toHaveBeenCalled();
    expect(harness.state.selection(promptAnswerKey(attention))?.answer).toBe("retry");
    expect(harness.state.isMasked(promptAnswerKey(attention))).toBe(false);
    expect(harness.failures).toHaveLength(1);
  });

  it("masks during delivery and refreshes only after a successful answer", async () => {
    const attention = question("step-1", "prompt-1");
    const sent = deferred<undefined>();
    const invalidate = vi.fn(async () => undefined);
    const harness = coordinatorHarness([[attention, draft("draft")]], {
      invalidateAttention: invalidate,
    });
    const attempt = submit(harness.coordinator, attention, "draft", async () => sent.promise);
    expect(harness.state.isMasked(promptAnswerKey(attention))).toBe(true);
    expect(invalidate).not.toHaveBeenCalled();
    sent.resolve(undefined);
    await attempt;
    expect(invalidate).toHaveBeenCalledOnce();
    expect(harness.state.selection(promptAnswerKey(attention))).toBeUndefined();
  });

  it("reports a failed success refresh without restoring an answered prompt", async () => {
    const attention = question("step-1", "prompt-1");
    const harness = coordinatorHarness([[attention, draft("answered")]], {
      invalidateAttention: async () => {
        throw new Error("refresh failed");
      },
    });
    await submit(harness.coordinator, attention, "answered", async () => undefined);
    expect(harness.state.selection(promptAnswerKey(attention))).toBeUndefined();
    expect(harness.failures).toEqual([expect.objectContaining({ kind: "refresh" })]);
  });

  it("reports delivery rejection even when the rejection value is undefined", async () => {
    const attention = question("step-1", "prompt-1");
    const harness = coordinatorHarness([[attention, draft("retry")]]);
    const send = vi.fn<() => Promise<void>>().mockRejectedValue(undefined);
    await submit(harness.coordinator, attention, "retry", send);
    expect(harness.state.selection(promptAnswerKey(attention))?.answer).toBe("retry");
    expect(harness.failures).toEqual([expect.objectContaining({ cause: undefined, kind: "delivery" })]);
  });

  it.each([
    ["step-2", "session-1"],
    ["step-1", "session-2"],
  ] as const)("isolates concurrent %s/%s identity collisions", async (stepID, sessionID) => {
    const first = question("step-1", "shared");
    const second = question(stepID, "shared", sessionID);
    const firstSend = deferred<undefined>();
    const secondSend = deferred<undefined>();
    expect(samePromptAnswerKey(promptAnswerKey(first), promptAnswerKey(second))).toBe(false);
    expect(taskDetailAttentionRowKey(first)).not.toBe(taskDetailAttentionRowKey(second));
    const harness = coordinatorHarness([
      [first, draft("first")],
      [second, draft("second")],
    ]);
    const firstAttempt = submit(harness.coordinator, first, "first", async () => firstSend.promise);
    const secondAttempt = submit(harness.coordinator, second, "second", async () => secondSend.promise);
    secondSend.reject(new Error("delivery lost"));
    await secondAttempt;
    expect(harness.state.isMasked(promptAnswerKey(first))).toBe(true);
    expect(harness.state.selection(promptAnswerKey(second))?.answer).toBe("second");
    firstSend.resolve(undefined);
    await firstAttempt;
    expect(harness.state.selection(promptAnswerKey(first))).toBeUndefined();
    expect(harness.state.selection(promptAnswerKey(second))?.answer).toBe("second");
  });

  it("does not restore a prompt resolved by the latest available projection", async () => {
    const attention = question("step-1", "prompt-1");
    const queryClient = new QueryClient();
    const answer = deferred<undefined>();
    const harness = coordinatorHarness([[attention, draft("discard")]], {
      queryClient,
    });
    const attempt = submit(harness.coordinator, attention, "discard", async () => answer.promise);
    queryClient.setQueryData(queryKeys.taskAttention("task-1"), { items: [], generatedAt: 2 });
    answer.reject(new Error("delivery lost"));
    await attempt;
    expect(harness.state.selection(promptAnswerKey(attention))).toBeUndefined();
    expect(harness.state.isMasked(promptAnswerKey(attention))).toBe(false);
    expect(harness.failures).toHaveLength(1);
  });

  it("discards state after unmount and identifies the Task on failure", async () => {
    const attention = question("step-1", "prompt-1");
    const answer = deferred<undefined>();
    let mounted = true;
    const harness = coordinatorHarness([[attention, draft("discard")]], {
      isMounted: () => mounted,
      task: { id: "task-1", shortID: "TASK-1", title: "Task title" },
    });
    const attempt = submit(harness.coordinator, attention, "discard", async () => answer.promise);
    const maskedState = harness.state;
    mounted = false;
    answer.reject(new Error("offline"));
    await attempt;
    expect(harness.state).toBe(maskedState);
    expect(harness.failures).toEqual([
      expect.objectContaining({ taskID: "task-1", taskShortID: "TASK-1", taskTitle: "Task title" }),
    ]);
  });
});

function coordinatorHarness(
  selections: readonly (readonly [QuestionAttentionItem, ReturnType<typeof draft>])[],
  options: Partial<CoordinatorOptions> = {},
) {
  let state = selections.reduce(
    (current, [attention, selection]) => current.withSelection(promptAnswerKey(attention), selection),
    emptyPromptAnswerState(),
  );
  const failures: unknown[] = [];
  const queryClient = options.queryClient ?? new QueryClient();
  const task = options.task ?? { id: "task-1", shortID: "TASK-1", title: "Task" };
  queryClient.setQueryData(queryKeys.taskAttention(task.id), {
    items: selections.map(([attention]) => attention),
    generatedAt: 1,
  });
  return {
    coordinator: new PromptAnswerCoordinator({
      queryClient,
      invalidateAttention: options.invalidateAttention ?? (async () => undefined),
      isMounted: options.isMounted ?? (() => true),
      notifyFailure: (failure) => failures.push(failure),
      task,
      updateState: (update) => {
        state = update(state);
      },
    }),
    failures,
    get state() {
      return state;
    },
  };
}
const draft = (answer: string) => withQuestionCommentary(emptyQuestionSelection(), answer);
async function submit(
  coordinator: PromptAnswerCoordinator,
  attention: QuestionAttentionItem,
  answer: string,
  send: () => Promise<void>,
) {
  return coordinator.submit({
    attention,
    selection: draft(answer),
    send: async () => {
      await send();
      return { results: [{ toolCallID: attention.question.toolCallID, outcome: "resolved" }] };
    },
  });
}
const baseQuestion = parsedQuestionAttention();
const question = (stepID: string, toolCallID: string, sessionID = "session-1"): QuestionAttentionItem => ({
  ...baseQuestion,
  question: { ...baseQuestion.question, toolCallID, sessionID, stepID },
});
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((nextResolve, nextReject) => {
    [resolve, reject] = [nextResolve, nextReject];
  });
  return { promise, reject, resolve };
}
