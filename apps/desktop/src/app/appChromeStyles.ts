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
    "h-[calc(var(--native-titlebar-height)*2)]",
] as const;

export const appChromeContrastScrimStyle = {
    background:
        "linear-gradient(to bottom, color-mix(in srgb, var(--background) 55%, transparent) 0%, transparent 100%)",
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
