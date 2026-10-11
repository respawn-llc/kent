import type { ApprovalDecision, PromptQuestionBatch } from "./models";

export type PromptIdentity = Readonly<{
  toolCallID: string;
  sessionID: string;
  stepID: string;
  batch?: PromptQuestionBatch;
}>;

export type FileAccessTarget = Readonly<{ requestedPath: string; resolvedPath: string }>;

export type OrdinaryQuestionPrompt = PromptIdentity &
  Readonly<{
    kind: "ordinary";
    suggestions: readonly string[];
    recommendedOptionIndex: number | null;
  }>;

export type ApprovalQuestionPrompt = PromptIdentity &
  Readonly<{
    kind: "approval";
    approvalDecisions: readonly ApprovalDecision[];
    accessTargets: readonly FileAccessTarget[];
  }>;

export type AttentionQuestionPrompt = OrdinaryQuestionPrompt | ApprovalQuestionPrompt;

export type PendingPrompt = AttentionQuestionPrompt &
  Readonly<{ question: string | null; createdAt: string }>;

export type PromptUpdate =
  Readonly<{ state: "pending"; prompt: PendingPrompt }> | Readonly<{ state: "resolved"; toolCallID: string }>;
