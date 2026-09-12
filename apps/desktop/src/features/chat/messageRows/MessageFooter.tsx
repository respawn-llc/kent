import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/ui";

import { TranscriptCopyAction } from "../transcriptRows/TranscriptCopyAction";

export function MessageFooter({
  text,
  committedAt,
  edit,
}: Readonly<{
  text: string;
  committedAt: number | null | undefined;
  edit?: ReactNode;
}>) {
  const { t } = useTranslation();
  const time = committedAt == null ? null : new Date(committedAt);
  return (
    <footer className="chat-message-footer">
      {time !== null && (
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <time dateTime={time.toISOString()} tabIndex={0}>
                {time.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" })}
              </time>
            </TooltipTrigger>
            <TooltipContent>
              {time.toLocaleString(undefined, { dateStyle: "full", timeStyle: "medium" })}
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>
      )}
      <div className="chat-message-actions">
        <TranscriptCopyAction
          copiedLabel={t("chatTranscript.copied")}
          copyLabel={t("chatTranscript.copy")}
          failureLabel={t("chatTranscript.copyFailed")}
          value={text}
        />
        {edit}
      </div>
    </footer>
  );
}
