import type { PromptIdentity, QuestionAttentionItem } from "@/api";
import type { QuestionSelectionState } from "./TaskDetailQuestionState";

export type PromptAnswerKey = Readonly<{
  sessionID: PromptIdentity["sessionID"];
  stepID: PromptIdentity["stepID"];
  toolCallID: PromptIdentity["toolCallID"];
}>;

export function promptAnswerKey(source: PromptIdentity | QuestionAttentionItem): PromptAnswerKey {
  const identity = "question" in source ? source.question : source;
  return Object.freeze({
    sessionID: identity.sessionID,
    stepID: identity.stepID,
    toolCallID: identity.toolCallID,
  });
}

export function samePromptAnswerKey(left: PromptAnswerKey, right: PromptAnswerKey): boolean {
  return (
    left.sessionID === right.sessionID && left.stepID === right.stepID && left.toolCallID === right.toolCallID
  );
}

export class PromptAnswerKeyMap<Value> implements Iterable<readonly [PromptAnswerKey, Value]> {
  private constructor(
    private readonly values: ReadonlyMap<string, ReadonlyMap<string, ReadonlyMap<string, Value>>>,
  ) {}

  static empty<Value>(): PromptAnswerKeyMap<Value> {
    return new PromptAnswerKeyMap(new Map());
  }

  get(key: PromptAnswerKey): Value | undefined {
    return this.values.get(key.sessionID)?.get(key.stepID)?.get(key.toolCallID);
  }

  has(key: PromptAnswerKey): boolean {
    return this.values.get(key.sessionID)?.get(key.stepID)?.has(key.toolCallID) === true;
  }

  with(key: PromptAnswerKey, value: Value): PromptAnswerKeyMap<Value> {
    const sessions = new Map(this.values);
    const steps = new Map(sessions.get(key.sessionID));
    const prompts = new Map(steps.get(key.stepID));
    prompts.set(key.toolCallID, value);
    steps.set(key.stepID, prompts);
    sessions.set(key.sessionID, steps);
    return new PromptAnswerKeyMap(sessions);
  }

  without(key: PromptAnswerKey): PromptAnswerKeyMap<Value> {
    const existingSteps = this.values.get(key.sessionID);
    const existingPrompts = existingSteps?.get(key.stepID);
    if (existingPrompts?.has(key.toolCallID) !== true) {
      return this;
    }
    const sessions = new Map(this.values);
    const steps = new Map(existingSteps);
    const prompts = new Map(existingPrompts);
    prompts.delete(key.toolCallID);
    if (prompts.size === 0) {
      steps.delete(key.stepID);
    } else {
      steps.set(key.stepID, prompts);
    }
    if (steps.size === 0) {
      sessions.delete(key.sessionID);
    } else {
      sessions.set(key.sessionID, steps);
    }
    return new PromptAnswerKeyMap(sessions);
  }

  *[Symbol.iterator](): Iterator<readonly [PromptAnswerKey, Value]> {
    for (const [sessionID, steps] of this.values) {
      for (const [stepID, prompts] of steps) {
        for (const [toolCallID, value] of prompts) {
          yield [{ sessionID, stepID, toolCallID }, value] as const;
        }
      }
    }
  }
}

export type PromptAnswerState = Readonly<{
  selection(key: PromptAnswerKey): QuestionSelectionState | undefined;
  isMasked(key: PromptAnswerKey): boolean;
}>;
