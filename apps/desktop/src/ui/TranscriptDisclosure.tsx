import { ChevronRight } from "lucide-react";
import { AnimatePresence, motion, useIsPresent, useReducedMotion } from "motion/react";
import { useId, useLayoutEffect, useRef, useState, type ReactNode } from "react";

import { cx } from "./classes";
import { motionDurationFromCSSVar } from "./motion";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "./radix/collapsible";
import "./TranscriptDisclosure.css";

export type TranscriptDisclosureIconTone = "neutral" | "warning" | "error" | "success";
export type TranscriptDisclosureSummaryMode = "single-line" | "multiline";

export type TranscriptDisclosureProps = Readonly<{
  actions?: ReactNode;
  body: ReactNode;
  collapseLabel: string;
  defaultExpanded: boolean;
  expandLabel: string;
  icon: ReactNode;
  iconTone?: TranscriptDisclosureIconTone | undefined;
  liveStatus?: ReactNode;
  summary: ReactNode;
  summaryMode?: TranscriptDisclosureSummaryMode | undefined;
  typeLabel?: ReactNode;
}>;

const iconToneClassNames: Readonly<Record<TranscriptDisclosureIconTone, string>> = {
  neutral: "transcript-disclosure-icon--neutral",
  warning: "transcript-disclosure-icon--warning",
  error: "transcript-disclosure-icon--error",
  success: "transcript-disclosure-icon--success",
};

export function TranscriptDisclosure({
  actions,
  body,
  collapseLabel,
  defaultExpanded,
  expandLabel,
  icon,
  iconTone = "neutral",
  liveStatus,
  summary,
  summaryMode = "single-line",
  typeLabel,
}: TranscriptDisclosureProps) {
  const bodyId = `transcript-disclosure-body-${useId()}`;
  const [expanded, setExpanded] = useState(defaultExpanded);

  // Adapted from Vercel AI Elements Reasoning (Apache-2.0):
  // https://github.com/vercel/ai-elements. Expansion is exclusively manual in Kent.
  return (
    <Collapsible
      open={expanded}
      onOpenChange={setExpanded}
      className="transcript-disclosure-shell group/transcript-disclosure relative w-full bg-transparent"
    >
      <TranscriptDisclosureHeader
        actions={actions}
        bodyId={bodyId}
        collapseLabel={collapseLabel}
        expanded={expanded}
        expandLabel={expandLabel}
        icon={icon}
        iconTone={iconTone}
        liveStatus={liveStatus}
        summary={summary}
        summaryMode={summaryMode}
        typeLabel={typeLabel}
      />
      <AnimatePresence initial={false}>
        {expanded ? (
          <DisclosureBody key="body" bodyId={bodyId}>
            {body}
          </DisclosureBody>
        ) : null}
      </AnimatePresence>
    </Collapsible>
  );
}

function DisclosureBody({ bodyId, children }: Readonly<{ bodyId: string; children: ReactNode }>) {
  const present = useIsPresent();
  const ref = useRef<HTMLDivElement>(null);
  const [height, setHeight] = useState<number | undefined>(undefined);
  const reducedMotion = useReducedMotion();
  useLayoutEffect(() => {
    const body = ref.current;
    if (body === null) return;
    const measure = () => {
      setHeight(body.getBoundingClientRect().height);
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(body);
    return () => {
      observer.disconnect();
    };
  }, []);
  return (
    <CollapsibleContent forceMount asChild id={bodyId}>
      <motion.div
        aria-hidden={present ? undefined : true}
        inert={present ? undefined : true}
        className="overflow-hidden"
        initial={reducedMotion ? false : { height: 0, opacity: 0 }}
        animate={{ height: height ?? "auto", opacity: 1 }}
        exit={{ height: 0, opacity: 0 }}
        transition={{ duration: reducedMotion ? 0 : motionDurationFromCSSVar("--motion-fast", 140) / 1000 }}
      >
        <div
          ref={ref}
          className="min-w-0 px-[var(--space-2)] pb-[var(--space-2)] text-sm text-[var(--color-on-background)]"
        >
          {children}
        </div>
      </motion.div>
    </CollapsibleContent>
  );
}

function TranscriptDisclosureHeader({
  actions,
  bodyId,
  collapseLabel,
  expanded,
  expandLabel,
  icon,
  iconTone,
  liveStatus,
  summary,
  summaryMode,
  typeLabel,
}: Readonly<{
  actions?: ReactNode;
  bodyId: string;
  collapseLabel: string;
  expanded: boolean;
  expandLabel: string;
  icon: ReactNode;
  iconTone: TranscriptDisclosureIconTone;
  liveStatus?: ReactNode;
  summary: ReactNode;
  summaryMode: TranscriptDisclosureSummaryMode;
  typeLabel?: ReactNode;
}>) {
  return (
    <header
      className={cx(
        "relative grid min-h-9 grid-cols-[auto_auto_minmax(0,1fr)_auto_auto] gap-[var(--space-2)] px-[var(--space-2)] py-[var(--space-1)]",
        summaryMode === "multiline" ? "items-start" : "items-center",
      )}
    >
      <CollapsibleTrigger
        aria-controls={bodyId}
        aria-expanded={expanded}
        aria-label={expanded ? collapseLabel : expandLabel}
        className="absolute inset-0 z-0 rounded-[var(--radius-s)] bg-transparent text-left outline-none focus-visible:ring-[2px] focus-visible:ring-[color-mix(in_srgb,var(--color-primary)_55%,transparent)] focus-visible:ring-offset-[-1px]"
        type="button"
      />
      <span
        aria-hidden="true"
        className={cx(
          "pointer-events-none relative z-0 grid size-5 shrink-0 place-items-center",
          iconToneClassNames[iconTone],
        )}
      >
        {icon}
      </span>
      {typeLabel === undefined ? (
        <span aria-hidden="true" className="pointer-events-none" />
      ) : (
        <span className="pointer-events-none relative z-0 min-w-0 truncate text-xs font-medium text-[var(--color-muted)]">
          {typeLabel}
        </span>
      )}
      <span
        className={cx(
          "pointer-events-none relative z-0 min-w-0 text-left text-sm text-[var(--color-on-background)]",
          summaryMode === "multiline" ? "transcript-disclosure-summary--multiline" : "truncate",
        )}
      >
        {summary}
      </span>
      <div className="pointer-events-none relative z-10 flex min-w-0 items-center justify-end gap-[var(--space-1)]">
        {liveStatus === undefined ? null : (
          <div className="pointer-events-auto flex min-w-0 items-center">{liveStatus}</div>
        )}
        {expanded && actions !== undefined ? (
          <div className="pointer-events-auto flex items-center">{actions}</div>
        ) : null}
      </div>
      <ChevronRight
        aria-hidden="true"
        className={cx(
          "transcript-disclosure-chevron pointer-events-none relative z-0 size-4 shrink-0 text-[var(--color-muted)]",
          expanded ? "rotate-90" : undefined,
        )}
      />
    </header>
  );
}
