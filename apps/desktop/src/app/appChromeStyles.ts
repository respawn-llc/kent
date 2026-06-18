import type { CSSProperties } from "react";

// Contrast scrim behind the top window chrome. On macOS the chrome reads against
// the native window glass (blur+tint); platforms without that glass (Linux,
// Windows, browser) get this gradient instead so the chrome controls/title stay
// legible over arbitrary content scrolling beneath them. Starts at a partial
// background-token fill at the very top and fades to transparent over the chrome.
export const appChromeContrastScrimClassNames = [
    "pointer-events-none",
    "fixed",
    "inset-x-0",
    "top-0",
    "z-10",
    "h-[calc(var(--native-titlebar-height)*1.5)]",
] as const;

// The bare `66%` between the two color stops is a color interpolation hint: it
// places the 50% midpoint of the fade at 66% of the height, so the alpha follows
// a smooth non-linear (power) curve rather than a straight linear ramp.
export const appChromeContrastScrimStyle = {
    background:
        "linear-gradient(to bottom, color-mix(in srgb, var(--background) 50%, transparent) 0%, 66%, transparent 100%)",
} satisfies CSSProperties;

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
