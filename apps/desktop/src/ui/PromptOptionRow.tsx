import { useId, useRef, type MouseEvent, type RefCallback } from "react";
import { useTranslation } from "react-i18next";
import { Star } from "lucide-react";
import { cx } from "./classes";
import { StaticMarkdown } from "./MarkdownText";
import { RadioGroupItem } from "./radix/radio-group";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "./radix/tooltip";

export function PromptOptionRow({
  disabled,
  primaryControlRef,
  recommended,
  text,
  value,
  selected = false,
  appearance = "plain",
  onActivate,
  className,
}: Readonly<{
  disabled: boolean;
  primaryControlRef?: RefCallback<HTMLButtonElement> | undefined;
  recommended: boolean;
  text: string;
  value: string;
  selected?: boolean;
  appearance?: "plain" | "picker";
  className?: string | undefined;
  onActivate?: () => void;
}>) {
  const { t } = useTranslation();
  const id = useId();
  const radioRef = useRef<HTMLButtonElement | null>(null);
  return (
    <div
      className={cx(
        "flex min-w-0 items-start gap-[var(--space-2)] text-left",
        appearance === "picker"
          ? "cursor-pointer rounded-[var(--radius-s)] px-[var(--space-1)] py-[var(--space-1)] text-[var(--color-muted)] transition-colors"
          : "text-[var(--color-on-island)]",
        disabled && "opacity-60",
        className,
      )}
      onClick={(event) => {
        if (disabled) return;
        if (onActivate !== undefined) {
          // Radix also clicks on keyboard focus. Only a pointer click confirms.
          if (event.detail !== 0 && !optionLink(event)) onActivate();
        } else {
          activateRadioFromOption(event, radioRef);
        }
      }}
    >
      <RadioGroupItem
        aria-labelledby={`${id}-label`}
        className="mt-1"
        disabled={disabled}
        id={id}
        ref={(element) => {
          radioRef.current = element;
          primaryControlRef?.(element);
        }}
        value={value}
      />
      <div
        className={cx(
          "min-w-0 flex-1 cursor-pointer",
          appearance === "plain" && recommended && "font-bold text-[var(--color-primary)]",
          appearance === "picker" && selected && "font-bold text-[var(--color-primary)]",
          appearance === "picker" && "markdown-inline-tail",
        )}
        id={`${id}-label`}
      >
        <StaticMarkdown value={text} />
        {recommended ? (
          appearance === "picker" ? (
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <span
                    className="ml-[var(--space-1)] inline-flex align-baseline text-[var(--color-primary)]"
                    onClick={(event) => {
                      event.stopPropagation();
                    }}
                  >
                    <Star className="size-3 fill-current" />
                  </span>
                </TooltipTrigger>
                <TooltipContent>{t("task.recommendedByAgent")}</TooltipContent>
              </Tooltip>
            </TooltipProvider>
          ) : (
            <span className="ml-[var(--space-2)] text-xs font-bold">({t("task.recommended")})</span>
          )
        ) : null}
      </div>
    </div>
  );
}

function optionLink(event: MouseEvent<HTMLDivElement>): boolean {
  let element = event.target instanceof Element ? event.target : null;
  while (element !== null && element !== event.currentTarget) {
    if (element instanceof HTMLAnchorElement || element.getAttribute("role") === "link") return true;
    element = element.parentElement;
  }
  return false;
}

function activateRadioFromOption(
  event: MouseEvent<HTMLDivElement>,
  radioRef: Readonly<{ current: HTMLButtonElement | null }>,
): void {
  if (optionLink(event)) return;
  let element = event.target instanceof Element ? event.target : null;
  while (element !== null && element !== event.currentTarget) {
    if (element instanceof HTMLButtonElement) return;
    element = element.parentElement;
  }
  radioRef.current?.click();
}
