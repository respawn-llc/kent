import { useId, useRef, type MouseEvent, type RefCallback } from "react";
import { useTranslation } from "react-i18next";
import { Badge } from "./Badge";
import { cx } from "./classes";
import { StaticMarkdown } from "./MarkdownText";
import { RadioGroupItem } from "./radix/radio-group";

export function PromptOptionRow({
  disabled,
  primaryControlRef,
  recommended,
  text,
  value,
  selected = false,
  appearance = "plain",
  onActivate,
}: Readonly<{
  disabled: boolean;
  primaryControlRef?: RefCallback<HTMLButtonElement> | undefined;
  recommended: boolean;
  text: string;
  value: string;
  selected?: boolean;
  appearance?: "plain" | "card";
  onActivate?: () => void;
}>) {
  const { t } = useTranslation();
  const id = useId();
  const radioRef = useRef<HTMLButtonElement | null>(null);
  return (
    <div
      className={cx(
        "flex min-w-0 items-start gap-[var(--space-2)] text-left text-[var(--color-on-island)]",
        appearance === "card" &&
          "cursor-pointer rounded-[var(--radius-m)] border border-[var(--color-outline)] p-[var(--space-3)] transition-colors duration-[var(--motion-fast)]",
        disabled && "opacity-60",
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
          appearance === "card" &&
            selected &&
            "text-[var(--color-primary)] [&_.markdown-text_:is(h1,h2,h3,h4,h5,h6)]:text-[var(--color-primary)]",
          appearance === "card" && "[&>.markdown-text]:inline [&>.markdown-text>div:last-child]:inline",
        )}
        id={`${id}-label`}
      >
        <StaticMarkdown value={text} />
        {recommended ? (
          appearance === "card" ? (
            <Badge
              className="ml-[var(--space-2)] border-[var(--color-success)] align-baseline"
              size="compact"
              tone="success"
            >
              {t("task.recommended")}
            </Badge>
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
