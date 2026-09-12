import { Brain } from "lucide-react";
import { useTranslation } from "react-i18next";

import type { TranscriptRenderItem } from "@/app-facade";
import { Shimmer, StreamingMarkdown, TranscriptDisclosure } from "@/ui";

import { TranscriptCopyAction } from "./TranscriptCopyAction";

export function TranscriptReasoningSlot({
  item,
}: Readonly<{ item: Extract<TranscriptRenderItem, { kind: "reasoning_trace" }> }>) {
  const { t, i18n } = useTranslation();
  const duration = item.state === "committed" ? (item.value.duration_ms ?? null) : null;
  const summary =
    item.state === "live" ? (
      <Shimmer>{t("chatTranscript.thinking")}</Shimmer>
    ) : duration !== null ? (
      t("chatTranscript.thoughtDuration", {
        seconds: new Intl.NumberFormat(i18n.language, { maximumFractionDigits: 1 }).format(duration / 1000),
      })
    ) : (
      item.value.CompactText
    );
  return (
    <TranscriptDisclosure
      actions={
        item.state === "committed" ? (
          <TranscriptCopyAction
            copiedLabel={t("chatTranscript.copied")}
            copyLabel={t("chatTranscript.copy")}
            failureLabel={t("chatTranscript.copyFailed")}
            value={item.value.Text}
          />
        ) : undefined
      }
      body={<StreamingMarkdown streaming={item.state === "live"} value={item.value.Text} />}
      collapseLabel={t("app.collapse")}
      defaultExpanded={false}
      expandLabel={t("app.expand")}
      icon={<Brain className="size-4" />}
      summary={summary}
    />
  );
}
