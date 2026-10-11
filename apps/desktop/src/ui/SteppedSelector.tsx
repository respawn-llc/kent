import * as Slider from "@radix-ui/react-slider";
import { useState, type CSSProperties, type ReactNode } from "react";
import "./steppedSelector.css";

type ActiveTone = "neutral" | "success" | "primary" | "error";

type SteppedSelectorProps<Value extends string> = Readonly<{
  values: readonly Value[];
  value: Value;
  disabled: boolean;
  activeTone(value: Value): ActiveTone;
  onCommit(value: Value): void;
  children(value: Value): ReactNode;
}>;

export function SteppedSelector<Value extends string>({
  values,
  value,
  disabled,
  activeTone,
  onCommit,
  children,
}: SteppedSelectorProps<Value>) {
  const [previous, setPrevious] = useState({ values, value, disabled });
  const [preview, setPreview] = useState<Value | null>(null);
  const replaced =
    previous.value !== value ||
    previous.disabled !== disabled ||
    previous.values.length !== values.length ||
    previous.values.some((candidate, index) => candidate !== values[index]);
  if (replaced) {
    setPrevious({ values, value, disabled });
    setPreview(null);
  }

  const displayedValue = (replaced ? null : preview) ?? value;
  const index = selectionIndex(values, displayedValue);
  const tone = activeTone(displayedValue);
  const style: CSSProperties & Record<"--stepped-active", string> = {
    "--stepped-active": toneColors[tone],
  };

  return (
    <div className="grid min-w-0">
      {children(displayedValue)}
      <Slider.Root
        className="app-region-no-drag stepped-selector relative flex h-[var(--space-5)] min-w-0 touch-none select-none items-center"
        disabled={disabled}
        max={values.length - 1}
        min={0}
        onLostPointerCapture={() => {
          setPreview(null);
        }}
        onPointerCancel={() => {
          setPreview(null);
        }}
        onValueChange={(next) => {
          setPreview(sliderValue(next, values));
        }}
        onValueCommit={(next) => {
          setPreview(null);
          onCommit(sliderValue(next, values));
        }}
        step={1}
        style={style}
        value={[index]}
      >
        <Slider.Track className="relative mx-[calc(var(--space-3)/2)] h-[var(--space-1)] grow rounded-full bg-[var(--color-outline)]">
          <Slider.Range className="stepped-selector-range absolute h-full rounded-full bg-[var(--stepped-active)]" />
          {values.map((step, stepIndex) => (
            <span
              className="pointer-events-none absolute top-1/2 h-[var(--space-1)] w-[var(--space-1)] -translate-1/2 rounded-full bg-[var(--color-on-island)]"
              key={step}
              style={{ left: `${String(stepPosition(stepIndex, values.length) * 100)}%` }}
            />
          ))}
        </Slider.Track>
        <Slider.Thumb className="stepped-selector-thumb block h-[var(--space-3)] w-[var(--space-3)] cursor-grab rounded-full bg-[var(--stepped-active)] outline-none active:cursor-grabbing data-disabled:cursor-not-allowed focus-visible:ring-[3px] focus-visible:ring-[color-mix(in_srgb,var(--color-primary)_40%,transparent)]" />
      </Slider.Root>
    </div>
  );
}

function sliderValue<Value extends string>(selection: readonly number[], values: readonly Value[]): Value {
  const index = selection[0];
  const value = index === undefined ? undefined : values[index];
  if (selection.length !== 1 || value === undefined) {
    throw new Error("Stepped selector received an invalid step.");
  }
  return value;
}

function selectionIndex<Value extends string>(values: readonly Value[], value: Value): number {
  const index = values.indexOf(value);
  if (values.length < 2 || index < 0 || new Set(values).size !== values.length) {
    throw new Error("Stepped selector requires at least two unique ordered values containing its selection.");
  }
  return index;
}

function stepPosition(index: number, count: number): number {
  return index / (count - 1);
}

const toneColors = {
  neutral: "var(--color-on-island)",
  success: "var(--color-success)",
  primary: "var(--color-primary)",
  error: "var(--color-error)",
} satisfies Record<ActiveTone, string>;
