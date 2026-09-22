import { MutationObserver, type QueryClient } from "@tanstack/react-query";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import * as Option from "effect/Option";
import type { TFunction } from "i18next";
import {
  errorMessage,
  type QuestionAnswerInput,
  type QuestionAttentionItem,
  type TaskAttention,
  type TaskDetail,
} from "@/api";
import { mutationCacheAtom, queryKeys, type AppServices, type StatusController } from "@/app-facade";
import {
  PromptAnswerKeyMap,
  promptAnswerKey,
  samePromptAnswerKey,
  type PromptAnswerKey,
  type PromptAnswerState,
} from "./PromptAnswerState";
import {
  canSubmitApprovalAnswer,
  canSubmitOrdinaryAnswer,
  type QuestionSelectionState,
} from "./TaskDetailQuestionState";
import { questionAnswerBatchInput } from "./TaskDetailQuestionAnswer";
import { promptSubmissionHandoff } from "./PromptSubmissionHandoff";
import type { PromptPrimaryFocusRequest } from "./PromptPrimaryControlRegistry";

export type TaskPromptAnswer = Readonly<{
  attention: QuestionAttentionItem;
  selection: QuestionSelectionState;
  input: QuestionAnswerInput;
}>;

class PromptSubmission {
  constructor(
    readonly owner: object,
    readonly answer: TaskPromptAnswer,
    readonly task: Readonly<{ id: string; shortID: string; title: string }>,
  ) {}
}

export function createTaskDetailPrompts({
  services,
  client,
  taskID,
  attention,
  detail,
  t,
  push,
}: Readonly<{
  services: AppServices;
  client: QueryClient;
  taskID: string;
  attention: Atom.Atom<{ readonly data: TaskAttention | undefined }>;
  detail(): TaskDetail | undefined;
  t: TFunction;
  push: StatusController["push"];
}>) {
  const owner = {};
  const mutationKey = ["task-prompt-answer", taskID];
  const pendingDeliveries = () =>
    client
      .getMutationCache()
      .findAll({ mutationKey, status: "pending" })
      .flatMap(({ state }) =>
        state.variables instanceof PromptSubmission && state.variables.owner === owner
          ? [state.variables]
          : [],
      );
  const pending = mutationCacheAtom(client, pendingDeliveries);
  const selections = Atom.writable<
    PromptAnswerKeyMap<QuestionSelectionState>,
    PromptAnswerKeyMap<QuestionSelectionState>
  >(
    (get) => {
      let current = Option.getOrElse(get.self<PromptAnswerKeyMap<QuestionSelectionState>>(), () =>
        PromptAnswerKeyMap.empty<QuestionSelectionState>(),
      );
      const latest = get(attention).data;
      if (latest !== undefined) {
        for (const [key] of current) {
          if (
            !latest.items.some(
              (item) => item.kind === "question" && samePromptAnswerKey(promptAnswerKey(item), key),
            )
          ) {
            current = current.without(key);
          }
        }
      }
      return current;
    },
    (ctx, value) => {
      ctx.setSelf(value);
    },
  );
  const state = Atom.make((get): PromptAnswerState => {
    const current = get(selections);
    const deliveries = get(pending);
    return {
      selection: (key) => current.get(key),
      isMasked: (key) =>
        deliveries.some(({ answer }) => samePromptAnswerKey(promptAnswerKey(answer.attention), key)),
    };
  });
  const focus = Atom.make<PromptPrimaryFocusRequest | undefined>(undefined);
  const editSelection = Atom.fn<Readonly<{ key: PromptAnswerKey; selection: QuestionSelectionState }>>()(
    (input) => Atom.update(selections, (current) => current.with(input.key, input.selection)),
  );
  const notify = (cause: unknown, input: PromptSubmission, kind: "delivery" | "refresh") => {
    const key = promptAnswerKey(input.answer.attention);
    push({
      body: `${input.task.shortID} · ${input.task.title}\n${errorMessage(cause)}`,
      durationMs: Infinity,
      id: ["task-prompt-answer", kind, taskID, key.sessionID, key.stepID, key.toolCallID].join(":"),
      title: t("states.error"),
      tone: "danger",
    });
  };
  const observer = new MutationObserver(client, {
    mutationKey,
    mutationFn: async (input: PromptSubmission) =>
      services.api.answerPromptBatch(questionAnswerBatchInput(input.answer.input)),
    onError: (error, input) => {
      notify(error, input, "delivery");
    },
    onSuccess: async (response, input) => {
      const key = promptAnswerKey(input.answer.attention);
      client.setQueryData<TaskAttention>(queryKeys.taskAttention(taskID), (current) =>
        current === undefined
          ? current
          : {
              ...current,
              items: current.items.filter(
                (item) =>
                  item.kind !== "question" ||
                  !response.results.some((result) =>
                    samePromptAnswerKey(promptAnswerKey(item), {
                      ...key,
                      toolCallID: result.toolCallID,
                    }),
                  ),
              ),
            },
      );
      try {
        await client.invalidateQueries({ exact: true, queryKey: queryKeys.taskAttention(taskID) });
      } catch (error) {
        notify(error, input, "refresh");
      }
    },
  });
  const answer = Atom.fn<TaskPromptAnswer>()(
    (input) =>
      Effect.gen(function* () {
        const task = detail();
        const latest = client.getQueryData<TaskAttention>(queryKeys.taskAttention(taskID));
        const key = promptAnswerKey(input.attention);
        const deliveries = pendingDeliveries();
        if (
          task === undefined ||
          !latest?.items.some(
            (item) => item.kind === "question" && samePromptAnswerKey(promptAnswerKey(item), key),
          ) ||
          deliveries.some((delivery) =>
            samePromptAnswerKey(promptAnswerKey(delivery.answer.attention), key),
          ) ||
          !validAnswer(input)
        )
          return;
        const handoff = promptSubmissionHandoff({
          attentionItems: latest.items.filter(
            (item) =>
              item.kind !== "question" ||
              !deliveries.some((delivery) =>
                samePromptAnswerKey(promptAnswerKey(item), promptAnswerKey(delivery.answer.attention)),
              ),
          ),
          submittedKey: key,
        });
        const request = observer.mutate(
          new PromptSubmission(owner, input, {
            id: task.id,
            shortID: task.shortID,
            title: task.title,
          }),
        );
        yield* Atom.set(focus, handoff.primaryFocusRequest);
        yield* Atom.update(selections, (current) => current.without(key));
        const result = yield* Effect.tryPromise(async () => request).pipe(Effect.result);
        if (result._tag === "Failure") {
          const current = client.getQueryData<TaskAttention>(queryKeys.taskAttention(taskID));
          if (
            current?.items.some(
              (item) => item.kind === "question" && samePromptAnswerKey(promptAnswerKey(item), key),
            )
          ) {
            yield* Atom.update(selections, (selections) =>
              selections.has(key) ? selections : selections.with(key, input.selection),
            );
          }
        }
      }),
    { concurrent: true },
  );
  const primaryFocus: Atom.Atom<PromptPrimaryFocusRequest | undefined> = focus;
  return { state, pending, focus: primaryFocus, editSelection, answer } as const;
}

function validAnswer({ input, attention }: TaskPromptAnswer): boolean {
  if (!samePromptAnswerKey(promptAnswerKey(input), promptAnswerKey(attention))) return false;
  if (input.kind === "ordinary") {
    return (
      attention.question.kind === "ordinary" &&
      canSubmitOrdinaryAnswer(input.selectedOptionNumber, input.freeformAnswer)
    );
  }
  return (
    attention.question.kind === "approval" &&
    attention.question.approvalDecisions.includes(input.decision) &&
    canSubmitApprovalAnswer(input.decision, input.commentary)
  );
}
