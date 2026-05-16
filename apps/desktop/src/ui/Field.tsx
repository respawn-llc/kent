import type { InputHTMLAttributes, ReactNode, SelectHTMLAttributes, TextareaHTMLAttributes } from "react";

import { cx } from "./classes";

type FieldShellProps = Readonly<{
  label: string;
  error?: string | undefined;
  hint?: ReactNode | undefined;
  children: ReactNode;
}>;

function FieldShell({ label, error, hint, children }: FieldShellProps) {
  return (
    <label className="ui-field">
      <span className="ui-field__label">{label}</span>
      {children}
      {hint !== undefined ? <span className="ui-field__hint">{hint}</span> : null}
      {error !== undefined ? <span className="ui-field__error">{error}</span> : null}
    </label>
  );
}

export type TextInputProps = Readonly<{
  label: string;
  error?: string | undefined;
  hint?: ReactNode | undefined;
}> &
  InputHTMLAttributes<HTMLInputElement>;

export function TextInput({ label, error, hint, className, ...props }: TextInputProps) {
  return (
    <FieldShell error={error} hint={hint} label={label}>
      <input className={cx("ui-input", className)} {...props} />
    </FieldShell>
  );
}

export type TextAreaProps = Readonly<{
  label: string;
  error?: string | undefined;
  hint?: ReactNode | undefined;
}> &
  TextareaHTMLAttributes<HTMLTextAreaElement>;

export function TextArea({ label, error, hint, className, ...props }: TextAreaProps) {
  return (
    <FieldShell error={error} hint={hint} label={label}>
      <textarea className={cx("ui-input", "ui-textarea", className)} {...props} />
    </FieldShell>
  );
}

export type SelectFieldProps = Readonly<{
  label: string;
  error?: string | undefined;
  hint?: ReactNode | undefined;
  children: ReactNode;
}> &
  SelectHTMLAttributes<HTMLSelectElement>;

export function SelectField({ label, error, hint, children, className, ...props }: SelectFieldProps) {
  return (
    <FieldShell error={error} hint={hint} label={label}>
      <select className={cx("ui-input", className)} {...props}>
        {children}
      </select>
    </FieldShell>
  );
}
