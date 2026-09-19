import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { useTranslation } from "react-i18next";

import type { ThinkingStatusPresentation } from "@/app-facade";
import { Spinner, motionDurationFromCSSVar } from "@/ui";

export function TranscriptThinkingStatus({
  presentation,
}: Readonly<{ presentation: ThinkingStatusPresentation | null }>) {
  const { t } = useTranslation();
  const reducedMotion = useReducedMotion();
  return (
    <AnimatePresence>
      {presentation === null ? null : (
        <motion.div
          key="status"
          className="flex min-w-0 items-start gap-[var(--space-2)] p-[var(--space-2)] text-sm text-[var(--color-on-background)]"
          initial={false}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, y: reducedMotion ? 0 : "var(--space-2)" }}
          transition={{ duration: reducedMotion ? 0 : motionDurationFromCSSVar("--motion-fast", 140) / 1000 }}
        >
          <Spinner
            className="mt-[calc((1lh-1rem)/2)] shrink-0"
            size="sm"
            tone={
              presentation.kind === "reviewing"
                ? "success"
                : presentation.kind === "compacting"
                  ? "secondary"
                  : "primary"
            }
          />
          <span className="line-clamp-2 min-w-0 [overflow-wrap:anywhere]">
            {presentation.kind === "text" ? presentation.text : t(`chatTranscript.${presentation.kind}`)}
          </span>
        </motion.div>
      )}
    </AnimatePresence>
  );
}
