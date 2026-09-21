import { Check } from "lucide-react";

import type { ChatSettingsAgentChoice } from "@/api";

import { SettingsRow } from "./ChatSettingsRows";

export function ChatSettingsAgentRow({
  agent,
  selected,
  transitionName,
  reason,
  onActivate,
}: Readonly<{
  agent: Pick<ChatSettingsAgentChoice, "role" | "model" | "thinking">;
  selected: boolean;
  transitionName: string;
  reason: string | undefined;
  onActivate(): void;
}>) {
  return (
    <SettingsRow as="button" onActivate={onActivate} reason={reason}>
      <div
        className="grid min-w-0 flex-1 text-left"
        style={{ viewTransitionName: selected ? transitionName : undefined }}
      >
        <span className="flex min-w-0 items-center gap-[var(--space-2)] font-bold text-[var(--color-on-island)]">
          <span className="truncate">{agent.role}</span>
          {selected ? <Check className="shrink-0" size={14} /> : null}
        </span>
        {agent.model !== null || agent.thinking !== null ? (
          <span className="truncate font-mono text-xs text-[var(--color-muted)]">
            {agent.model} {agent.thinking}
          </span>
        ) : null}
      </div>
    </SettingsRow>
  );
}
