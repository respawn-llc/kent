import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { cx } from "@/ui";
import { TranscriptCopyAction } from "./TranscriptCopyAction";

export function TranscriptDiagnosticRow({
  text,
  icon,
  tone,
}: Readonly<{ text: string; icon: ReactNode; tone: "error" | "warning" }>) {
  const { t } = useTranslation();
  return (
    <div className="flex min-w-0 items-start gap-[var(--space-2)] px-[var(--space-2)] text-sm">
      <span
        className={cx(
          "shrink-0",
          tone === "error" ? "text-[var(--color-error)]" : "text-[var(--color-warning)]",
        )}
      >
        {icon}
      </span>
      <p className="chat-transcript-row-body min-w-0 flex-1 text-[var(--color-muted)]">
        {text.length > 600 ? `${text.slice(0, 600)}…` : text}
      </p>
      <TranscriptCopyAction
        value={text}
        copiedLabel={t("chatTranscript.copied")}
        copyLabel={t("chatTranscript.copy")}
        failureLabel={t("chatTranscript.copyFailed")}
      />
    </div>
  );
}
