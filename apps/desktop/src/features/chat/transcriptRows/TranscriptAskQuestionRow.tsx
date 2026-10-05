import { CornerDownRight, MessageCircleQuestionMark, Star } from "lucide-react";
import { useId } from "react";
import { useTranslation } from "react-i18next";

import type { ChatTranscriptCommittedRow } from "@/api";
import { RadioGroup, RadioGroupItem, StaticMarkdown } from "@/ui";

import { TranscriptCopyAction } from "./TranscriptCopyAction";
import {
  askQuestionCopyText,
  isAskQuestionToolRow,
  type TranscriptTool,
  type TranscriptToolPresentation,
} from "./transcriptAskQuestionPolicy";

export function TranscriptAskQuestionRow({ row }: Readonly<{ row: ChatTranscriptCommittedRow }>) {
  const { t } = useTranslation();
  if (!isAskQuestionToolRow(row) || row.Visibility === "hidden") return null;
  const tool = row.Tool;
  const presentation = tool.Presentation;
  const copyText = askQuestionCopyText(row);
  return (
    <div className="chat-transcript-question-row text-sm">
      <MessageCircleQuestionMark
        className={`mt-1 size-4 shrink-0 ${tool.IsError ? "text-[var(--color-error)]" : "text-[var(--color-success)]"}`}
      />
      <div className="min-w-0">
        {!tool.IsError && tool.QuestionAnswer == null ? (
          <p className="chat-transcript-row-body">{copyText}</p>
        ) : (
          <AskQuestionBody presentation={presentation} tool={tool} />
        )}
      </div>
      <TranscriptCopyAction
        value={copyText}
        copyLabel={t("chatTranscript.copy")}
        copiedLabel={t("chatTranscript.copied")}
        failureLabel={t("chatTranscript.copyFailed")}
      />
    </div>
  );
}

function AskQuestionBody({
  presentation,
  tool,
}: Readonly<{
  tool: TranscriptTool;
  presentation: TranscriptToolPresentation;
}>) {
  const answer = tool.QuestionAnswer;
  const selectedOptionNumber = tool.IsError ? null : (answer?.SelectedOptionNumber ?? null);
  return (
    <div className="chat-transcript-question-body">
      <StaticMarkdown value={presentation.Question} />
      <QuestionOptions
        isError={tool.IsError}
        recommendedOptionIndex={presentation.RecommendedOptionIndex}
        selectedOptionNumber={selectedOptionNumber}
        suggestions={presentation.Suggestions}
      />
      <QuestionResponse answer={answer} isError={tool.IsError} text={tool.Text} />
    </div>
  );
}

function QuestionResponse({
  answer,
  isError,
  text,
}: Readonly<{
  answer: NonNullable<NonNullable<ChatTranscriptCommittedRow["Tool"]>["QuestionAnswer"]> | null | undefined;
  isError: boolean;
  text: string;
}>) {
  if (isError) {
    return <p className="chat-transcript-row-body chat-transcript-question-error">{text}</p>;
  }
  if (answer?.Freeform === undefined || answer.Freeform === null) return null;
  return <QuestionAnswerText text={answer.Freeform} />;
}

function QuestionOptions({
  isError,
  recommendedOptionIndex,
  selectedOptionNumber,
  suggestions,
}: Readonly<{
  isError: boolean;
  recommendedOptionIndex: number;
  selectedOptionNumber: number | null;
  suggestions: readonly string[];
}>) {
  const groupID = useId();
  if ((!isError && selectedOptionNumber === null) || suggestions.length === 0) return null;
  return (
    <RadioGroup
      className="chat-transcript-question-options"
      disabled
      value={selectedOptionNumber?.toString() ?? null}
    >
      {suggestions.map((suggestion, index) => {
        const optionNumber = index + 1;
        const selected = selectedOptionNumber === optionNumber;
        const recommended = recommendedOptionIndex === optionNumber;
        return (
          <div
            className={
              selected
                ? "chat-transcript-question-option chat-transcript-question-option--selected"
                : "chat-transcript-question-option"
            }
            key={`${String(optionNumber)}-${suggestion}`}
          >
            <RadioGroupItem
              aria-labelledby={`${groupID}-${String(optionNumber)}`}
              className="mt-1 disabled:cursor-default disabled:opacity-100"
              value={String(optionNumber)}
            />
            <span className="shrink-0">{optionNumber}.</span>
            <div
              className="chat-transcript-question-option-label markdown-inline-tail"
              id={`${groupID}-${String(optionNumber)}`}
            >
              <StaticMarkdown value={suggestion} />
              {recommended ? (
                <span className="whitespace-nowrap">
                  {"\u00a0"}
                  <Star className="inline size-3 fill-current align-baseline" />
                </span>
              ) : null}
            </div>
          </div>
        );
      })}
    </RadioGroup>
  );
}

function QuestionAnswerText({ text }: Readonly<{ text: string }>) {
  return (
    <div className="chat-transcript-question-answer">
      <CornerDownRight className="mt-0.5 size-4 shrink-0" />
      <span className="whitespace-pre-wrap break-words select-text">{text}</span>
    </div>
  );
}
