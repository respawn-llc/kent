import type { ButtonHTMLAttributes, ReactNode } from "react";

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
    variant = "secondary",
    type = "button",
    ...props
}: ButtonProps) {
    return (
        <button
            className={cx(
                "rounded-[var(--radius-m)] border border-[var(--color-outline)] bg-[var(--color-island-1)] px-[10px] py-[4px] text-[var(--color-on-island)] disabled:cursor-not-allowed disabled:opacity-55",
                variant === "primary" &&
                "border-transparent bg-[var(--color-primary)] text-[var(--color-on-primary)]",
                variant === "danger" && "border-[var(--color-error)] text-[var(--color-error)]",
                variant === "ghost" && "border-transparent bg-transparent",
                className,
            )}
            type={type}
            {...props}
        >
            {children}
        </button>
    );
}
