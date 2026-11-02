import { Brain } from "lucide-react";
import { useTranslation } from "react-i18next";

import type { TranscriptReasoningDisplayGroup } from "@/app-facade";
import { Shimmer, TranscriptDisclosure } from "@/ui";
import { StreamingMarkdown } from "@/ui/markdown";

import { TranscriptCopyAction } from "./TranscriptCopyAction";

export function TranscriptReasoningSlot({ item }: Readonly<{ item: TranscriptReasoningDisplayGroup }>) {
  const { t, i18n } = useTranslation();
  const first = item.members[0];
  const live = item.members.some((member) => member.state === "live");
  const duration = item.members.reduce<number | null>((total, member) => {
    if (member.state !== "committed" || member.value.duration_ms == null) return total;
    return (total ?? 0) + member.value.duration_ms;
  }, null);
  const compactPreview =
    item.members.find((member) => member.value.CompactText !== "")?.value.CompactText ??
    first.value.CompactText;
  const text = item.members.map((member) => member.value.Text).join("\n\n");
  const summary = live ? (
    <Shimmer>{t("chatTranscript.thinking")}</Shimmer>
  ) : duration !== null ? (
    t("chatTranscript.thoughtDuration", {
      seconds: new Intl.NumberFormat(i18n.language, { maximumFractionDigits: 1 }).format(duration / 1000),
    })
  ) : (
    compactPreview
  );
  return (
    <TranscriptDisclosure
      actions={
        !live ? (
          <TranscriptCopyAction
            copiedLabel={t("chatTranscript.copied")}
            copyLabel={t("chatTranscript.copy")}
            failureLabel={t("chatTranscript.copyFailed")}
            value={text}
          />
        ) : undefined
      }
      body={
        <div className="text-[var(--color-muted)]">
          <StreamingMarkdown streaming={live} value={text} />
        </div>
      }
      collapseLabel={t("app.collapse")}
      defaultExpanded={false}
      expandLabel={t("app.expand")}
      icon={<Brain className="size-4" />}
      summary={summary}
    />
  );
}
