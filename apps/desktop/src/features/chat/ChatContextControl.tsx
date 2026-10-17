import { useState, type CSSProperties, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Button, DisabledInteractionGuard, InteractiveChip, HoverPopover } from "@/ui";
import { contextPresentation } from "./chatContextPresentation";
import "./chatContext.css";
import { ComposerIcon } from "./ComposerIcon";

type Props = Readonly<{
  used: number;
  window: number | null;
  policyDisabled: boolean;
  compacting: boolean;
  compact(): void;
  onOpenChange?(open: boolean): void;
  subscriptionUsage?: ReactNode;
}>;

export function ChatContextControl(props: Props) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const presentation = contextPresentation(props.used, props.window);
  const available = presentation !== null;
  const disabledReason = props.policyDisabled
    ? t("chatComposer.context.disabled")
    : props.compacting
      ? t("chatComposer.context.compacting")
      : undefined;
  return (
    <HoverPopover
      open={open && available}
      onOpenChange={(value) => {
        setOpen(value && available);
        props.onOpenChange?.(value && available);
      }}
      side="top"
      align="end"
      className="chat-context-popover w-80 text-sm text-[var(--color-muted)]"
      trigger={(triggerProps) => (
        <InteractiveChip {...triggerProps} variant="ghost" className="chat-context-trigger">
          <ContextMeter compacting={props.compacting} presentation={presentation} />
        </InteractiveChip>
      )}
    >
      {presentation !== null && (
        <>
          <div className="chat-context-progress">
            <strong className="chat-context-percent">{presentation.usedPercent}%</strong>
            <span className="chat-context-used">{presentation.used}</span>
            <div className="chat-context-track">
              <div
                className="chat-context-gradient"
                style={{ clipPath: `inset(0 ${((1 - presentation.extent) * 100).toString()}% 0 0)` }}
              />
            </div>
            <span className="chat-context-total">{presentation.window}</span>
          </div>
          <DisabledInteractionGuard disabled={disabledReason !== undefined} reason={disabledReason}>
            <Button
              className="w-full"
              disabled={disabledReason !== undefined}
              onClick={() => {
                setOpen(false);
                props.compact();
              }}
            >
              {t("chatComposer.context.compact")}
            </Button>
          </DisabledInteractionGuard>
          {props.subscriptionUsage}
        </>
      )}
    </HoverPopover>
  );
}

function ContextMeter({
  compacting,
  presentation,
}: Readonly<{
  compacting: boolean;
  presentation: ReturnType<typeof contextPresentation>;
}>) {
  const { t } = useTranslation();
  const ringStyle: CSSProperties & Record<"--chat-context-angle", string> = {
    "--chat-context-angle": `${((presentation?.extent ?? 0) * 360).toString()}deg`,
  };
  if (compacting)
    return (
      <>
        <span>{t("chatComposer.context.compacting")}</span>
        <ComposerIcon kind="loading" className="text-[var(--color-secondary)]" />
      </>
    );
  return (
    <>
      <span>{presentation?.usedPercent ?? 0}%</span>
      <span className="chat-context-circle chat-context-ring-track">
        <span className="chat-context-ring" style={ringStyle} />
      </span>
    </>
  );
}
