import { useTranslation } from "react-i18next";

import type { ChatSettingsThinking } from "@/api";

import { SteppedSelector } from "@/ui";

export function ThinkingSelector({
  thinking,
  onCommit,
}: Readonly<{
  thinking: Extract<ChatSettingsThinking, { kind: "enumerated" }>;
  onCommit(value: string): void;
}>) {
  return (
    <SteppedSelector
      activeTone={thinkingTone}
      disabled={thinking.editability.kind !== "editable"}
      onCommit={onCommit}
      value={thinking.value}
      values={thinking.values}
    >
      {(value) => <ThinkingHeading value={value} />}
    </SteppedSelector>
  );
}

export function ThinkingHeading({ value }: Readonly<{ value: string }>) {
  const { t } = useTranslation();
  return (
    <div className="flex min-w-0 items-center gap-[var(--space-3)]">
      <span className="min-w-0 flex-1 truncate">{t("chatSettings.thinking")}</span>
      <span className="shrink-0 font-mono text-[var(--color-muted)]">{value}</span>
    </div>
  );
}

function thinkingTone(value: string): "neutral" | "success" | "primary" | "error" {
  switch (value) {
    case "low":
      return "success";
    case "medium":
    case "high":
      return "primary";
    case "xhigh":
    case "max":
    case "ultra":
      return "error";
    default:
      return "neutral";
  }
}
