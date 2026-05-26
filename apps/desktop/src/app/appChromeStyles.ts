export const appChromeTitleClassNames = [
    "pointer-events-none",
    "fixed",
    "top-[8px]",
    "z-30",
    "h-6",
    "max-w-[min(520px,45vw)]",
    "truncate",
    "text-[12pt]",
    "font-medium",
    "leading-6",
    "text-[var(--color-on-island)]",
] as const;

export const appChromeInlineTitleClassNames = [
    "pointer-events-none",
    "ml-[var(--space-2)]",
    "h-6",
    "max-w-[min(520px,45vw)]",
    "truncate",
    "text-[12pt]",
    "font-medium",
    "leading-6",
    "text-left",
    "text-[var(--color-on-island)]",
] as const;

export function appChromeTitlePlacementClassNames(macOS: boolean): readonly string[] {
    return macOS ? ["right-[var(--space-2)]", "text-right"] : ["left-[var(--space-2)]", "text-left"];
}
