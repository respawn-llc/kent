import { useTranslation } from "react-i18next";

import type { ChatSettingsAgent } from "@/api";

import { SettingsRow } from "./ChatSettingsRows";

export function ChatSettingsAgentRow({
  agent,
  selected,
  transitionName,
  reason,
  onActivate,
}: Readonly<{
  agent: ChatSettingsAgent;
  selected: boolean;
  transitionName: string;
  reason: string | undefined;
  onActivate(): void;
}>) {
  const { t } = useTranslation();
  return (
    <SettingsRow as="button" onActivate={onActivate} reason={reason}>
      <div
        className="grid min-w-0 flex-1 text-left"
        style={{ viewTransitionName: selected ? transitionName : undefined }}
      >
        <span className="flex min-w-0 items-center gap-[var(--space-2)] font-bold text-[var(--color-on-island)]">
          <span className="truncate">{t("chatSettings.agentName", { name: agent.role })}</span>
        </span>
        <span className="truncate font-mono text-xs text-[var(--color-muted)]">
          {agent.model} {agent.thinking}
        </span>
      </div>
    </SettingsRow>
  );
}
