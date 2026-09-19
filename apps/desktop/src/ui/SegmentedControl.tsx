import * as RadioGroupPrimitive from "@radix-ui/react-radio-group";
import type { CSSProperties, ReactNode } from "react";

import { cx } from "./classes";

export type SegmentedControlOption<Value extends string> = Readonly<{
  disabled?: boolean;
  label: ReactNode;
  value: Value;
}>;

export type SegmentedControlProps<Value extends string> = Readonly<{
  size?: "default" | "compact";
  ariaLabel: string;
  className?: string | undefined;
  disabled?: boolean;
  onValueActivate?(value: Value): void;
  onValueChange(value: Value): void;
  options: readonly SegmentedControlOption<Value>[];
  value: Value;
}>;

export function SegmentedControl<Value extends string>({
  ariaLabel,
  className,
  disabled = false,
  onValueActivate,
  onValueChange,
  options,
  value,
  size = "default",
}: SegmentedControlProps<Value>) {
  const optionValues = new Set<Value>();
  let selectedSegment: Readonly<{
    index: number;
    option: SegmentedControlOption<Value>;
  }> | null = null;
  for (const [index, option] of options.entries()) {
    if (optionValues.has(option.value)) {
      throw new Error(`Segmented control "${ariaLabel}" contains duplicate value "${option.value}".`);
    }
    optionValues.add(option.value);
    if (option.value === value) {
      selectedSegment = { index, option };
    }
  }
  if (selectedSegment === null) {
    throw new Error(`Segmented control "${ariaLabel}" has no option for value "${value}".`);
  }
  if (selectedSegment.option.disabled === true) {
    throw new Error(`Segmented control "${ariaLabel}" cannot select disabled value "${value}".`);
  }
  const padding = size === "compact" ? "3px" : "var(--space-1)";
  const indicatorStyle = {
    top: padding,
    bottom: padding,
    left: padding,
    transform: `translateX(${(selectedSegment.index * 100).toString()}%)`,
    width: `calc((100% - (${padding} * 2)) / ${options.length.toString()})`,
  } satisfies CSSProperties;
  return (
    <RadioGroupPrimitive.Root
      aria-label={ariaLabel}
      className={cx(
        "app-region-no-drag relative inline-grid h-[var(--space-6)] min-w-0 grid-flow-col auto-cols-fr rounded-[var(--radius-m)] border border-[var(--color-outline)] bg-[var(--color-island-1)] p-[var(--space-1)]",
        size === "compact" &&
          "data-[size=compact]:h-[22px] data-[size=compact]:p-[3px] [&>button]:min-w-8 [&>button]:px-[6px]",
        className,
      )}
      data-size={size}
      disabled={disabled}
      onValueChange={(nextValue) => {
        const option = options.find((candidate) => candidate.value === nextValue);
        if (option === undefined) {
          throw new Error(`Segmented control "${ariaLabel}" received unknown value "${nextValue}".`);
        }
        onValueChange(option.value);
      }}
      orientation="horizontal"
      value={value}
    >
      <span
        aria-hidden="true"
        className="pointer-events-none absolute rounded-[calc(var(--radius-m)-var(--space-1))] bg-[var(--color-island-3)] transition-[transform,width] duration-[var(--motion-fast)] ease-out motion-reduce:transition-none"
        style={indicatorStyle}
      />
      {options.map((option) => (
        <RadioGroupPrimitive.Item
          className="relative z-10 h-full min-h-0 min-w-9 rounded-[calc(var(--radius-m)-var(--space-1))] bg-transparent px-[var(--space-2)] text-xs font-extrabold text-[var(--color-muted)] outline-none transition-[color,opacity] duration-[var(--motion-fast)] motion-reduce:transition-none hover:text-[var(--color-on-island)] focus-visible:ring-[3px] focus-visible:ring-[color-mix(in_srgb,var(--color-primary)_40%,transparent)] disabled:cursor-not-allowed disabled:opacity-45 data-[state=checked]:text-[var(--color-on-island)] [@media(pointer:coarse)]:min-w-11"
          disabled={option.disabled}
          key={option.value}
          onClick={() => {
            onValueActivate?.(option.value);
          }}
          value={option.value}
        >
          {option.label}
        </RadioGroupPrimitive.Item>
      ))}
    </RadioGroupPrimitive.Root>
  );
}
