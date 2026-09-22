import { useTranslation } from "react-i18next";
import { CircleX, Gavel } from "lucide-react";

import type { ChatTranscriptCommittedRow } from "@/api";
import { StaticMarkdown } from "@/ui";

import { reviewerFeedbackCopyText } from "./transcriptReviewerPolicy";
import { TranscriptFlatRow } from "./TranscriptFlatRow";
import { TranscriptDiagnosticRow } from "./TranscriptDiagnosticRow";

export function TranscriptReviewerRow({ row }: Readonly<{ row: ChatTranscriptCommittedRow }>) {
  const { t } = useTranslation();
  if (row.Kind !== "reviewer_feedback" && row.Kind !== "reviewer_error") return null;
  if (row.Visibility === "hidden") return null;

  if (row.Kind === "reviewer_feedback") {
    if (row.ReviewerFeedback === null) throw new Error("Reviewer feedback row is missing its payload.");
    const suggestions = [...row.ReviewerFeedback.Suggestions];
    return (
      <TranscriptFlatRow
        body={
          <div className="chat-transcript-reviewer-suggestions">
            {suggestions.map((suggestion, index) => (
              <div className="chat-transcript-reviewer-suggestion" key={`${String(index + 1)}-${suggestion}`}>
                <span className="chat-transcript-reviewer-number">{index + 1}.</span>
                <StaticMarkdown value={suggestion} />
              </div>
            ))}
          </div>
        }
        copyText={reviewerFeedbackCopyText(suggestions)}
        defaultExpanded={false}
        icon={<Gavel className="size-4 text-[var(--color-secondary)]" />}
        iconTone="neutral"
        summary={t("chatTranscript.reviewerSuggestions", { count: row.ReviewerFeedback.SuggestionCount })}
      />
    );
  }
  if (row.ReviewerError === null) throw new Error("Reviewer error row is missing its payload.");
  return (
    <TranscriptDiagnosticRow
      text={row.ReviewerError.Detail}
      icon={<CircleX className="size-4" />}
      tone="error"
    />
  );
}
