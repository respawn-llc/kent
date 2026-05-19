import type { ButtonHTMLAttributes, CSSProperties, ReactNode } from "react";

import { cx } from "./classes";

export type ButtonVariant = "primary" | "secondary" | "ghost" | "danger";

export type ButtonProps = Readonly<{
    children: ReactNode;
    variant?: ButtonVariant;
}> &
    ButtonHTMLAttributes<HTMLButtonElement>;

export function Button({
    children,
    className,
    style,
    variant = "secondary",
    type = "button",
    ...props
}: ButtonProps) {
    return (
        <button
            className={cx(
                "rounded-[var(--radius-m)] border border-[var(--button-border)] bg-[var(--button-bg)] px-[10px] py-[4px] text-[var(--button-color)] disabled:cursor-not-allowed disabled:opacity-55",
                className,
            )}
            style={{ ...buttonVariantStyles[variant], ...style }}
            type={type}
            {...props}
        >
            {children}
        </button>
    );
}

type ButtonVariantStyle = CSSProperties & Record<"--button-bg" | "--button-border" | "--button-color", string>;

const buttonVariantStyles = {
    danger: {
        "--button-bg": "var(--color-island-1)",
        "--button-border": "var(--color-error)",
        "--button-color": "var(--color-error)",
    },
    ghost: {
        "--button-bg": "transparent",
        "--button-border": "transparent",
        "--button-color": "var(--color-on-island)",
    },
    primary: {
        "--button-bg": "var(--color-primary)",
        "--button-border": "transparent",
        "--button-color": "var(--color-on-primary)",
    },
    secondary: {
        "--button-bg": "var(--color-island-1)",
        "--button-border": "var(--color-outline)",
        "--button-color": "var(--color-on-island)",
    },
} satisfies Record<ButtonVariant, ButtonVariantStyle>;
