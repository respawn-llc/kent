import { useRef, useState, type ReactNode } from "react";
import { Trans, useTranslation } from "react-i18next";
import {
  Button,
  DisabledInteractionGuard,
  InteractiveChip,
  Popover,
  PopoverContent,
  PopoverTrigger,
  Spinner,
} from "@/ui";
import { contextPresentation } from "./chatContextPresentation";
import "./chatContext.css";

type Props = Readonly<{
  used: number;
  window: number | null;
  autoCompactionControl: ReactNode;
  policyDisabled: boolean;
  completedCount: number;
  compacting: boolean;
  compact(): void;
}>;

export function ChatContextControl(props: Props) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const hoverOpened = useRef(false);
  const presentation = contextPresentation(props.used, props.window);
  const available = presentation !== null;
  const disabledReason = props.policyDisabled
    ? t("chatComposer.context.disabled")
    : props.compacting
      ? t("chatComposer.context.compacting")
      : undefined;
  return (
    <Popover
      open={open && available}
      onOpenChange={(value) => {
        if (value) hoverOpened.current = false;
        setOpen(value && available);
      }}
    >
      <PopoverTrigger asChild>
        <InteractiveChip
          className="chat-context-trigger"
          onClick={(event) => {
            if (open && hoverOpened.current) {
              hoverOpened.current = false;
              event.preventDefault();
            }
          }}
          onPointerEnter={(event) => {
            if (event.pointerType !== "mouse" || !available || open) return;
            hoverOpened.current = true;
            setOpen(true);
          }}
        >
          <ContextMeter compacting={props.compacting} presentation={presentation} />
        </InteractiveChip>
      </PopoverTrigger>
      {presentation !== null && (
        <PopoverContent
          side="top"
          align="end"
          className="chat-context-popover text-sm text-[var(--color-muted)]"
          onOpenAutoFocus={(event) => {
            if (hoverOpened.current) event.preventDefault();
          }}
          onCloseAutoFocus={(event) => {
            if (hoverOpened.current) event.preventDefault();
          }}
        >
          <div>
            <Trans
              i18nKey="chatComposer.context.remaining"
              values={{
                tokens: presentation.remaining,
                percent: presentation.remainingPercent,
                window: presentation.window,
              }}
              components={{ strong: <strong /> }}
            />
          </div>
          {props.autoCompactionControl}
          <div>
            <Trans
              i18nKey="chatComposer.context.completed"
              values={{ count: props.completedCount }}
              components={{ strong: <strong /> }}
            />
          </div>
          <div className="chat-context-bottom">
            <div className="chat-context-track">
              <div
                className="chat-context-gradient"
                style={{ clipPath: `inset(0 ${((1 - presentation.extent) * 100).toString()}% 0 0)` }}
              />
            </div>
            <DisabledInteractionGuard disabled={disabledReason !== undefined} reason={disabledReason}>
              <Button
                disabled={disabledReason !== undefined}
                onClick={() => {
                  setOpen(false);
                  props.compact();
                }}
              >
                {t("chatComposer.context.compact")}
              </Button>
            </DisabledInteractionGuard>
          </div>
        </PopoverContent>
      )}
    </Popover>
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
  if (compacting)
    return (
      <>
        <span>{t("chatComposer.context.compacting")}</span>
        <Spinner tone="secondary" size="sm" className="chat-context-circle" />
      </>
    );
  return (
    <>
      <span>{presentation?.usedPercent ?? 0}%</span>
      <span className="chat-context-circle chat-context-ring-track">
        <span
          className="chat-context-ring"
          style={{
            maskImage: `conic-gradient(black ${((presentation?.extent ?? 0) * 360).toString()}deg, transparent 0)`,
          }}
        />
      </span>
    </>
  );
}
