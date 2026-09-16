import { useRef, useState } from "react";
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
  autoCompactionEnabled: boolean;
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
              }}
              components={{ strong: <strong /> }}
            />
          </div>
          <div>
            <Trans
              i18nKey={
                props.policyDisabled
                  ? "chatComposer.context.disabled"
                  : props.autoCompactionEnabled
                    ? "chatComposer.context.autoOn"
                    : "chatComposer.context.autoOff"
              }
              components={{ strong: <strong /> }}
            />
          </div>
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
  return (
    <>
      <span
        className="chat-context-trigger-content"
        style={{ visibility: compacting ? "visible" : "hidden" }}
      >
        <span>{t("chatComposer.context.compacting")}</span>
        {compacting ? (
          <Spinner tone="secondary" size="sm" className="h-5 w-5" />
        ) : (
          <span className="h-5 w-5" />
        )}
      </span>
      <span
        className="chat-context-trigger-content"
        style={{ visibility: compacting ? "hidden" : "visible" }}
      >
        <span>{presentation?.usedPercent ?? 0}%</span>
        <svg width="20" height="20" viewBox="0 0 20 20" fill="none">
          <circle cx="10" cy="10" r="8" stroke="var(--color-outline)" strokeWidth="2" />
          <circle
            className="chat-context-ring"
            cx="10"
            cy="10"
            r="8"
            pathLength="1"
            stroke="var(--color-secondary)"
            strokeWidth="2"
            strokeDasharray={`${(presentation?.extent ?? 0).toString()} 1`}
            transform="rotate(-90 10 10)"
          />
        </svg>
      </span>
    </>
  );
}
