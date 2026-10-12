import { motion } from "motion/react";
import type { Ref } from "react";

import type { ChatSettingsAgentChoice } from "@/api";

import { SettingsRow } from "./ChatSettingsRows";
import { motionDurationFromCSSVar } from "@/ui";

export function ChatSettingsAgentRow({
  agent,
  selected,
  transitionName,
  reason,
  onActivate,
  contentRef,
}: Readonly<{
  agent: Pick<ChatSettingsAgentChoice, "role" | "model" | "thinking">;
  selected: boolean;
  transitionName: string;
  reason: string | undefined;
  onActivate(): void;
  contentRef?: Ref<HTMLDivElement> | undefined;
}>) {
  return (
    <SettingsRow as="button" onActivate={onActivate} reason={reason}>
      <motion.div
        ref={contentRef}
        className="grid min-w-0 flex-1 text-left"
        {...(selected ? { layoutId: transitionName } : {})}
        initial={selected ? false : { opacity: 0 }}
        animate={{ opacity: 1 }}
        transition={{
          opacity: { duration: motionDurationFromCSSVar("--motion-fast", 140) / 1000 },
          layout: {
            duration: motionDurationFromCSSVar("--motion-fast", 140) / 1000,
            ease: [0.22, 1, 0.36, 1],
          },
        }}
      >
        <span className="flex min-w-0 items-center gap-[var(--space-2)] font-bold text-[var(--color-on-island)]">
          <span className="truncate">{agent.role}</span>
        </span>
        {agent.model !== null || agent.thinking !== null ? (
          <span className="truncate font-mono text-xs text-[var(--color-muted)]">
            {agent.model} {agent.thinking}
          </span>
        ) : null}
      </motion.div>
    </SettingsRow>
  );
}
