import { act, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { PromptAnswerBatchResponse } from "@/api";
import type { FakeRpcTransport } from "@/test-support/api";
import { projectEventsFixture } from "@/test-support/project-events";

import { appI18n } from "@/i18n";
import {
  mountTaskDetailSurface,
  promptAnswerBatchRoute,
  taskDetailResponse,
  taskQuestionPage as taskAttentionMany,
  taskAttentionRoute as attentionRoute,
} from "@/test-support/task-detail";

describe("Task Detail live refresh", () => {
  it("preserves progression and rejects an equal-timestamp older overlapping reconciliation", async () => {
    let attention = taskAttentionMany([
      ["ask-1", 1],
      ["ask-2", 1],
    ]);
    const first = deferred<undefined>();
    const second = deferred<undefined>();
    const staleAttention = taskAttentionMany([["ask-2", 1]], 5);
    const staleRead = deferred<ReturnType<typeof taskAttention>>();
    let answerCount = 0;
    let attentionReadCount = 0;
    mountTaskDetailSurface(taskDetailResponse, {
      routes: [
        attentionRoute(async () => {
          attentionReadCount += 1;
          return attentionReadCount === 2 ? staleRead.promise : attention;
        }),
        promptAnswerBatchRoute(async () => {
          answerCount += 1;
          if (answerCount === 1) {
            await first.promise;
            attention = staleAttention;
            return answered("ask-1");
          }
          await second.promise;
          attention = taskAttentionMany([], 5);
          return answered("ask-2");
        }),
      ],
    });
    const user = userEvent.setup();

    await waitFor(() => {
      expect(screen.getAllByRole("radio")).toHaveLength(4);
    });
    const submits = screen.getAllByRole("button", { name: appI18n.t("task.submitAnswer") });
    await user.click(submits.reduce((first) => first));

    await waitFor(() => {
      expect(screen.queryByText("ask-1")).not.toBeInTheDocument();
      expect(screen.getByText("ask-2")).toBeInTheDocument();
      expect(screen.getAllByRole("radio")[0]).toHaveFocus();
    });
    await user.click(screen.getByRole("button", { name: appI18n.t("task.submitAnswer") }));
    expect(answerCount).toBe(2);
    await waitFor(() => {
      expect(screen.queryAllByRole("radio")).toHaveLength(0);
    });

    first.resolve(undefined);
    await waitFor(() => {
      expect(attentionReadCount).toBe(2);
    });
    second.resolve(undefined);
    await waitFor(() => {
      expect(attentionReadCount).toBe(3);
    });
    await act(async () => {
      staleRead.resolve(staleAttention);
    });
    expect(screen.queryAllByRole("radio")).toHaveLength(0);
  });
  it("skips a masked prompt when handing focus to the next actionable question", async () => {
    mountTaskDetailSurface(taskDetailResponse, {
      routes: [
        attentionRoute(() => taskAttentionWithOneOption("ask-1", "ask-2", "ask-3")),
        promptAnswerBatchRoute(async () => new Promise<PromptAnswerBatchResponse>(() => undefined)),
      ],
    });
    const user = userEvent.setup();
    await waitFor(() => {
      expect(screen.getAllByRole("radio")).toHaveLength(6);
    });
    const submits = screen.getAllByRole("button", { name: appI18n.t("task.submitAnswer") });
    await user.click(requiredElement(submits, 1));
    await waitFor(() => {
      expect(screen.queryByText("ask-2")).not.toBeInTheDocument();
    });
    await user.click(requiredElement(screen.getAllByRole("radio", { name: "option-1 (Recommended)" }), 0));
    await user.click(
      requiredElement(screen.getAllByRole("button", { name: appI18n.t("task.submitAnswer") }), 0),
    );
    await waitFor(() => {
      expect(screen.getByText("ask-3")).toBeInTheDocument();
      expect(screen.getAllByRole("radio")[0]).toHaveFocus();
    });
  });
  it("preserves an equal-timestamp background refresh over an earlier reconciliation", async () => {
    let attention = taskAttention("ask-1", 1);
    const delivery = deferred<undefined>();
    const staleRead = deferred<ReturnType<typeof taskAttention>>();
    const backgroundRead = deferred<ReturnType<typeof taskAttention>>();
    let attentionReadCount = 0;
    const services = mountTaskDetailSurface(taskDetailResponse, {
      routes: [
        attentionRoute(async () => {
          attentionReadCount += 1;
          if (attentionReadCount === 2) return staleRead.promise;
          if (attentionReadCount === 3) return backgroundRead.promise;
          return attention;
        }),
        promptAnswerBatchRoute(async () => {
          await delivery.promise;
          return answered("ask-1");
        }),
      ],
    });
    const user = userEvent.setup();
    await waitForQuestionOptionCount(1);
    await user.click(screen.getByRole("button", { name: appI18n.t("task.submitAnswer") }));
    delivery.resolve(undefined);
    await waitFor(() => {
      expect(attentionReadCount).toBe(2);
    });

    await waitForProjectSubscription(services.transport);
    attention = taskAttentionMany([], 5);
    act(() => {
      projectEventsFixture(services.transport).emit({ action: "question_cleared" });
    });
    await waitFor(() => {
      expect(attentionReadCount).toBe(3);
    });
    await act(async () => {
      backgroundRead.resolve(attention);
    });
    await act(async () => {
      staleRead.resolve(taskAttentionMany([["ask-1", 1]], 5));
    });
    expect(screen.queryAllByRole("radio")).toHaveLength(0);
  });

  it("cancels a pre-answer background refresh before authoritative reconciliation", async () => {
    const delivery = deferred<undefined>();
    const backgroundRead = deferred<ReturnType<typeof taskAttention>>();
    const directRead = deferred<ReturnType<typeof taskAttention>>();
    let attentionReadCount = 0;
    const services = mountTaskDetailSurface(taskDetailResponse, {
      routes: [
        attentionRoute(async () => {
          attentionReadCount += 1;
          if (attentionReadCount === 2) return backgroundRead.promise;
          if (attentionReadCount === 3) return directRead.promise;
          return taskAttention("ask-1", 1);
        }),
        promptAnswerBatchRoute(async () => {
          await delivery.promise;
          return answered("ask-1");
        }),
      ],
    });
    const user = userEvent.setup();
    await waitForQuestionOptionCount(1);
    await waitForProjectSubscription(services.transport);
    act(() => {
      projectEventsFixture(services.transport).emit({ action: "question_cleared" });
    });
    await waitFor(() => {
      expect(attentionReadCount).toBe(2);
    });

    await user.click(screen.getByRole("button", { name: appI18n.t("task.submitAnswer") }));
    delivery.resolve(undefined);
    await waitFor(() => {
      expect(attentionReadCount).toBe(3);
    });
    await act(async () => {
      directRead.resolve(taskAttentionMany([], 5));
    });
    await act(async () => {
      backgroundRead.resolve(taskAttentionMany([["ask-1", 1]], 4));
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
    expect(screen.queryAllByRole("radio")).toHaveLength(0);
  });

  it("does not move focus when the intended next prompt disappears and the earlier prompt restores", async () => {
    let attention = taskAttentionWithOneOption("ask-1", "ask-2", "ask-3");
    const answer = deferred<PromptAnswerBatchResponse>();
    const services = mountTaskDetailSurface(taskDetailResponse, {
      routes: [attentionRoute(() => attention), promptAnswerBatchRoute(async () => answer.promise)],
    });
    const user = userEvent.setup();
    await waitFor(() => {
      expect(screen.getAllByRole("radio")).toHaveLength(6);
    });
    const submits = screen.getAllByRole("button", { name: appI18n.t("task.submitAnswer") });
    await user.click(submits.reduce((first) => first));
    await waitFor(() => {
      expect(screen.queryByText("ask-1")).not.toBeInTheDocument();
      expect(screen.getByText("ask-2")).toBeInTheDocument();
      expect(screen.getAllByRole("radio")[0]).toHaveFocus();
    });
    attention = taskAttentionWithOneOption("ask-1", "ask-3");
    act(() => {
      projectEventsFixture(services.transport).emit({ action: "question_waiting" });
    });
    await waitFor(() => {
      expect(screen.queryByText("ask-2")).not.toBeInTheDocument();
    });
    const beforeFailure = services.transport.descriptorCalls.length;
    answer.reject(new Error("delivery failed"));
    await waitFor(() => {
      expect(screen.getByText("ask-1")).toBeInTheDocument();
      expect(screen.queryByText("ask-2")).not.toBeInTheDocument();
      const radios = screen.getAllByRole("radio");
      expect(radios).toHaveLength(4);
      expect(radios[0]).not.toHaveFocus();
      expect(radios[2]).not.toHaveFocus();
    });
    expect(services.transport.descriptorCalls).toHaveLength(beforeFailure);
  });

  it("shows the next batch question when its waiting event arrives after an answer", async () => {
    let attention = taskAttention("ask-1", 1);
    const services = mountTaskDetailSurface(taskDetailResponse, {
      routes: [
        attentionRoute(() => attention),
        promptAnswerBatchRoute(() => {
          attention = taskAttention("ask-2", 2);
          return answered("ask-1");
        }),
      ],
    });

    await waitForQuestionOptionCount(1);
    await waitForProjectSubscription(services.transport);
    const subscriptionStarts = projectEventsFixture(services.transport).startCount;
    expect(subscriptionStarts).toBeGreaterThan(0);
    await services.api.answerPromptBatch({
      sessionID: "33333333-3333-4333-8333-333333333333",
      stepID: "22222222-2222-4222-8222-222222222222",
      entries: [
        {
          kind: "question",
          toolCallID: "ask-1",
          selectedOptionNumber: null,
          freeform: "answered",
        },
      ],
    });

    act(() => {
      projectEventsFixture(services.transport).emit({
        action: "question_waiting",
        relatedIDs: ["33333333-3333-4333-8333-333333333333", "ask-2"],
      });
    });

    await waitForQuestionOptionCount(2);
    expect(projectEventsFixture(services.transport).startCount).toBe(subscriptionStarts);
  });

  it("shows page recovery after observation failure until explicit Retry opens that observation", async () => {
    let attention = taskAttention("ask-1", 1);
    const services = mountTaskDetailSurface(taskDetailResponse, {
      routes: [attentionRoute(() => attention)],
    });

    await waitForQuestionOptionCount(1);
    await waitForProjectSubscription(services.transport);
    const starts = projectEventsFixture(services.transport).startCount;
    act(() => {
      projectEventsFixture(services.transport).fail(new Error("offline"));
    });
    attention = taskAttention("ask-2", 2);
    await screen.findByTestId("error-state");
    expect(screen.queryAllByRole("radio")).toHaveLength(0);
    expect(projectEventsFixture(services.transport).startCount).toBe(starts);
    const retry = await screen.findByRole("button", { name: appI18n.t("app.retry") });
    act(() => {
      retry.click();
    });
    await waitForProjectSubscription(services.transport);
    act(() => {
      projectEventsFixture(services.transport).open();
    });

    await waitForQuestionOptionCount(2);
  });
});

function answered(toolCallID: string): PromptAnswerBatchResponse {
  return { results: [{ toolCallID, outcome: "resolved" }] };
}

function taskAttention(askID: string, optionCount: number) {
  return taskAttentionMany([[askID, optionCount]]);
}

const taskAttentionWithOneOption = (...askIDs: readonly string[]) =>
  taskAttentionMany(askIDs.map((askID) => [askID, 1] as const));

async function waitForProjectSubscription(transport: FakeRpcTransport) {
  await waitFor(() => {
    expect(projectEventsFixture(transport).activeCount).toBeGreaterThan(0);
  });
}

async function waitForQuestionOptionCount(count: number) {
  await waitFor(() => {
    expect(screen.getAllByRole("radio")).toHaveLength(count + 1);
  });
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((nextResolve, nextReject) => {
    [resolve, reject] = [nextResolve, nextReject];
  });
  return { promise, reject, resolve };
}

function requiredElement(elements: readonly HTMLElement[], index: number): HTMLElement {
  const element = elements[index];
  if (element === undefined) throw new Error(`Required test element ${index.toString()} is unavailable`);
  return element;
}
