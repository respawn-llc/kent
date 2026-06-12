import { cx } from "./classes";

export type SpinnerProps = Readonly<{
  className?: string | undefined;
  size?: "sm" | "md";
  strokeWidth?: number | undefined;
  testID?: string | undefined;
}>;

export function Spinner({ className, size = "md", strokeWidth = 3, testID = "spinner" }: SpinnerProps) {
  return (
    <svg
      aria-hidden="true"
      className={cx(
        "motion-safe:animate-spin text-[var(--color-primary)]",
        size === "sm" ? "h-4 w-4" : "h-7 w-7",
        className,
      )}
      data-testid={testID}
      fill="none"
      viewBox="0 0 24 24"
    >
      <path
        d="M 12 3 A 9 9 0 1 1 3 12"
        stroke="currentColor"
        strokeLinecap="round"
        strokeWidth={strokeWidth}
      />
    </svg>
  );
}
