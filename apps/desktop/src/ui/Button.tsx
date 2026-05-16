import type { ButtonHTMLAttributes, ReactNode } from "react";

import { cx } from "./classes";

export type ButtonVariant = "primary" | "secondary" | "ghost" | "danger";

export type ButtonProps = Readonly<{
  children: ReactNode;
  variant?: ButtonVariant;
}> &
  ButtonHTMLAttributes<HTMLButtonElement>;

export function Button({ children, className, variant = "secondary", type = "button", ...props }: ButtonProps) {
  return (
    <button className={cx("ui-button", `ui-button--${variant}`, className)} type={type} {...props}>
      {children}
    </button>
  );
}
