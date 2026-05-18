import { cx } from "./classes";

export type SpinnerProps = Readonly<{
  className?: string | undefined;
  size?: "sm" | "md";
  testID?: string | undefined;
}>;

export function Spinner({ className, size = "md", testID = "spinner" }: SpinnerProps) {
  return (
    <div
      className={cx(
        "motion-safe:animate-spin rounded-full border-[3px] border-[var(--color-outline)] border-t-[var(--color-primary)]",
        size === "sm" ? "h-4 w-4" : "h-7 w-7",
        className,
      )}
      data-testid={testID}
    />
  );
}
