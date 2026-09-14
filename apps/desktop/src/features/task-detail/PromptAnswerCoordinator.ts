import type { QuestionAttentionItem } from "@/api";
import { promptAnswerKey, samePromptAnswerKey, type PromptAnswerState } from "./PromptAnswerState";
import type { QuestionSelectionState } from "./TaskDetailQuestionState";

export type PromptAnswerFailure = Readonly<{
  cause: unknown;
  kind: "delivery" | "refresh";
  promptKey: ReturnType<typeof promptAnswerKey>;
  taskID: string;
  taskShortID: string;
  taskTitle: string;
}>;

type PromptAnswerCoordinatorDependencies = Readonly<{
  invalidateAttention(): Promise<void>;
  isMounted(): boolean;
  notifyFailure(failure: PromptAnswerFailure): void;
  currentAttention(): readonly QuestionAttentionItem[] | undefined;
  task: Readonly<{ id: string; shortID: string; title: string }>;
  updateState(update: (state: PromptAnswerState) => PromptAnswerState): void;
}>;

export class PromptAnswerCoordinator {
  constructor(private readonly dependencies: PromptAnswerCoordinatorDependencies) {}

  async submit({
    attention,
    selection,
    send,
  }: Readonly<{
    attention: QuestionAttentionItem;
    selection: QuestionSelectionState;
    send(): Promise<unknown>;
  }>): Promise<void> {
    const key = promptAnswerKey(attention);
    this.dependencies.updateState((state) =>
      state.withSelection(key, selection).beginSubmission(key, attention),
    );

    try {
      await send();
    } catch (error: unknown) {
      if (this.dependencies.isMounted()) {
        const latest = this.dependencies.currentAttention();
        const resolved =
          latest !== undefined && !latest.some((item) => samePromptAnswerKey(promptAnswerKey(item), key));
        this.dependencies.updateState((state) =>
          resolved ? state.discardSubmission(key) : state.restoreSubmission(key),
        );
      }
      this.notify("delivery", error, key);
      return;
    }
    if (this.dependencies.isMounted()) {
      this.dependencies.updateState((state) => state.discardSubmission(key));
    }
    try {
      await this.dependencies.invalidateAttention();
    } catch (error: unknown) {
      this.notify("refresh", error, key);
    }
  }

  private notify(
    kind: PromptAnswerFailure["kind"],
    cause: unknown,
    promptKey: PromptAnswerFailure["promptKey"],
  ): void {
    this.dependencies.notifyFailure({
      cause,
      kind,
      promptKey,
      taskID: this.dependencies.task.id,
      taskShortID: this.dependencies.task.shortID,
      taskTitle: this.dependencies.task.title,
    });
  }
}
