import { RegistryProvider, useAtomValue } from "@effect/atom-react";
import { QueryClient } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import type { QuestionAttentionItem, PromptAnswerBatchResponse } from "@/api";
import { queryKeys } from "@/app-facade";
import { appI18n } from "@/i18n";
import { deferred } from "@/test-support/chat-runtime";
import {
  createTaskDetailTestServices,
  parsedQuestionAttention,
  taskDetailResponse,
} from "@/test-support/task-detail";
import { createTaskDetailViewModel, useTaskDetailActions, useTaskDetailReads } from "./TaskDetailViewModel";
import { emptyQuestionSelection, withQuestionCommentary } from "./TaskDetailQuestionState";
import { promptAnswerKey } from "./PromptAnswerState";

it("admits an exact prompt only once in a turn and restores its answer on delivery failure without a repair read", async () => {
  const attention = parsedQuestionAttention();
  const view = setup([attention]);
  await waitFor(() => {
    expect(view.result.current.reads.detail.isSuccess).toBe(true);
  });
  const selection = withQuestionCommentary(emptyQuestionSelection(), "Answer draft");
  const input = submission(attention, selection);
  act(() => {
    view.result.current.actions.answer(input);
    view.result.current.actions.answer(input);
  });
  await waitFor(() => {
    expect(view.send).toHaveBeenCalledTimes(1);
  });
  expect(view.result.current.state.isMasked(promptAnswerKey(attention))).toBe(true);
  const reads = view.read.mock.calls.length;
  await act(async () => view.deliveries[0]?.reject(new Error("delivery failed")));
  await waitFor(() => {
    expect(view.result.current.state.isMasked(promptAnswerKey(attention))).toBe(false);
  });
  expect(view.result.current.state.selection(promptAnswerKey(attention))).toEqual(selection);
  expect(view.read).toHaveBeenCalledTimes(reads);
  expect(view.push).toHaveBeenCalledOnce();
});

it.each(["step", "session"] as const)(
  "restores only the failed exact prompt across a concurrent %s identity collision",
  async (identity) => {
    const first = parsedQuestionAttention();
    const second: QuestionAttentionItem = {
      ...first,
      id: "second-attention",
      question: {
        ...first.question,
        ...(identity === "step" ? { stepID: "other-step" } : { sessionID: "other-session" }),
      },
    };
    const view = setup([first, second]);
    await waitFor(() => {
      expect(view.result.current.reads.detail.isSuccess).toBe(true);
    });
    const firstInput = submission(first);
    const secondInput = submission(second, withQuestionCommentary(emptyQuestionSelection(), "Second draft"));
    act(() => {
      view.result.current.actions.answer(firstInput);
      view.result.current.actions.answer(secondInput);
    });
    await waitFor(() => {
      expect(view.send).toHaveBeenCalledTimes(2);
    });
    await act(async () => view.deliveries[1]?.reject(new Error("second failed")));
    await waitFor(() => {
      expect(view.result.current.state.isMasked(promptAnswerKey(first))).toBe(true);
      expect(view.result.current.state.selection(promptAnswerKey(second))).toEqual(secondInput.selection);
    });
    await act(async () =>
      view.deliveries[0]?.resolve({
        results: [{ toolCallID: first.question.toolCallID, outcome: "resolved" }],
      }),
    );
    await waitFor(() => {
      expect(view.result.current.state.isMasked(promptAnswerKey(first))).toBe(false);
    });
    expect(view.result.current.reads.attention.data?.items).toEqual([second]);
    expect(view.result.current.state.selection(promptAnswerKey(first))).toBeUndefined();
    expect(view.result.current.state.selection(promptAnswerKey(second))).toEqual(secondInput.selection);
  },
);

it("reports the originating Task after leaving without restoring its answer in a reopened destination", async () => {
  const attention = parsedQuestionAttention();
  const view = setup([attention]);
  await waitFor(() => {
    expect(view.result.current.reads.detail.isSuccess).toBe(true);
  });
  act(() => {
    view.result.current.actions.answer(submission(attention));
  });
  await waitFor(() => {
    expect(view.send).toHaveBeenCalledOnce();
  });
  const reopened = createTaskDetailViewModel({
    services: view.services,
    client: view.client,
    taskID: "task-1",
    enabled: true,
    t: appI18n.t,
    push: view.push,
  });
  view.rerender({ model: reopened });
  expect(view.result.current.state.isMasked(promptAnswerKey(attention))).toBe(false);
  await act(async () => view.deliveries[0]?.reject(new Error("delivery failed")));
  await waitFor(() => {
    expect(view.push).toHaveBeenCalledOnce();
  });
  expect(view.result.current.state.selection(promptAnswerKey(attention))).toBeUndefined();
  expect(view.push).toHaveBeenCalledWith(
    expect.objectContaining({
      id: [
        "task-prompt-answer",
        "delivery",
        "task-1",
        attention.question.sessionID,
        attention.question.stepID,
        attention.question.toolCallID,
      ].join(":"),
    }),
  );
});

it("does not restore an exact prompt removed by the latest attention projection", async () => {
  const attention = parsedQuestionAttention();
  const view = setup([attention]);
  await waitFor(() => {
    expect(view.result.current.reads.detail.isSuccess).toBe(true);
  });
  act(() => {
    view.result.current.actions.answer(submission(attention));
  });
  await waitFor(() => {
    expect(view.send).toHaveBeenCalledOnce();
  });
  act(() => {
    view.client.setQueryData(queryKeys.taskAttention("task-1"), { items: [], generatedAt: 2 });
  });
  await act(async () => view.deliveries[0]?.reject(new Error("delivery failed")));
  await waitFor(() => {
    expect(view.push).toHaveBeenCalledOnce();
  });
  expect(view.result.current.state.selection(promptAnswerKey(attention))).toBeUndefined();
  expect(view.result.current.reads.attention.data?.items).toEqual([]);
});

function setup(attention: readonly QuestionAttentionItem[]) {
  const services = createTaskDetailTestServices(taskDetailResponse);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(queryKeys.taskAttention("task-1"), { items: attention, generatedAt: 1 });
  const read = vi
    .spyOn(services.api, "listTaskAttention")
    .mockImplementation(
      async () => client.getQueryData(queryKeys.taskAttention("task-1")) ?? { items: [], generatedAt: 1 },
    );
  const deliveries: ReturnType<typeof deferred<PromptAnswerBatchResponse>>[] = [];
  const send = vi.spyOn(services.api, "answerPromptBatch").mockImplementation(async () => {
    const delivery = deferred<PromptAnswerBatchResponse>();
    deliveries.push(delivery);
    return delivery.promise;
  });
  const push = vi.fn();
  const model = createTaskDetailViewModel({
    services,
    client,
    taskID: "task-1",
    enabled: true,
    t: appI18n.t,
    push,
  });
  const view = renderHook(
    ({ model }) => ({
      reads: useTaskDetailReads(model, true),
      actions: useTaskDetailActions(model),
      state: useAtomValue(model.prompts.state),
    }),
    {
      initialProps: { model },
      wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider>,
    },
  );
  return { ...view, model, services, client, send, deliveries, read, push };
}

function submission(
  attention: QuestionAttentionItem,
  selection = withQuestionCommentary(emptyQuestionSelection(), "Answer"),
) {
  return {
    attention,
    selection,
    input: {
      kind: "ordinary" as const,
      sessionID: attention.question.sessionID,
      stepID: attention.question.stepID,
      toolCallID: attention.question.toolCallID,
      selectedOptionNumber: selection.selectedOption,
      freeformAnswer: selection.answer,
    },
  };
}
