import {
  useId,
  type InputHTMLAttributes,
  type ReactNode,
  type SelectHTMLAttributes,
  type TextareaHTMLAttributes,
} from "react";

import { cx } from "./classes";

export type FieldError = string | readonly string[];

type FieldShellProps = Readonly<{
  label: string;
  error?: FieldError | undefined;
  hint?: ReactNode | undefined;
  inputId: string;
  errorId: string;
  hintId: string;
  children: ReactNode;
}>;

function FieldShell({ label, error, hint, inputId, errorId, hintId, children }: FieldShellProps) {
  const errors = normalizeErrors(error);

  return (
    <div className="grid gap-[var(--space-3)]">
      <label className="text-[0.82rem] font-extrabold text-[var(--color-muted)]" htmlFor={inputId}>
        {label}
      </label>
      {children}
      {hint !== undefined ? (
        <span className="text-[var(--color-muted)]" id={hintId}>
          {hint}
        </span>
      ) : null}
      <span
        aria-live="polite"
        className="grid overflow-hidden opacity-0 transition-[grid-template-rows,opacity] duration-[var(--motion-normal)] data-[visible=true]:grid-rows-[1fr] data-[visible=true]:opacity-100 grid-rows-[0fr]"
        data-visible={errors.length > 0 ? "true" : "false"}
        id={errorId}
      >
        <span className="grid min-h-0 gap-[var(--space-1)]">
          {errors.map((message) => (
            <span className="text-[var(--color-error)]" key={message}>
              {message}
            </span>
          ))}
        </span>
      </span>
    </div>
  );
}

export type TextInputProps = Readonly<{
  label: string;
  error?: FieldError | undefined;
  hint?: ReactNode | undefined;
}> &
  InputHTMLAttributes<HTMLInputElement>;

export function TextInput({ label, error, hint, className, ...props }: TextInputProps) {
  const generatedId = useId();
  const inputId = props.id ?? generatedId;
  const hintId = `${inputId}-hint`;
  const errorId = `${inputId}-error`;

  return (
    <FieldShell error={error} errorId={errorId} hint={hint} hintId={hintId} inputId={inputId} label={label}>
      <input
        aria-describedby={`${hintId} ${errorId}`}
        aria-invalid={error === undefined ? undefined : true}
        className={cx(inputClassName, className)}
        {...props}
        id={inputId}
      />
    </FieldShell>
  );
}

export type TextAreaProps = Readonly<{
  label: string;
  error?: FieldError | undefined;
  hint?: ReactNode | undefined;
}> &
  TextareaHTMLAttributes<HTMLTextAreaElement>;

export function TextArea({ label, error, hint, className, ...props }: TextAreaProps) {
  const generatedId = useId();
  const inputId = props.id ?? generatedId;
  const hintId = `${inputId}-hint`;
  const errorId = `${inputId}-error`;

  return (
    <FieldShell error={error} errorId={errorId} hint={hint} hintId={hintId} inputId={inputId} label={label}>
      <textarea
        aria-describedby={`${hintId} ${errorId}`}
        aria-invalid={error === undefined ? undefined : true}
        className={cx(inputClassName, "min-h-24 resize-y", className)}
        {...props}
        id={inputId}
      />
    </FieldShell>
  );
}

export type SelectFieldProps = Readonly<{
  label: string;
  error?: FieldError | undefined;
  hint?: ReactNode | undefined;
  children: ReactNode;
}> &
  SelectHTMLAttributes<HTMLSelectElement>;

export function SelectField({ label, error, hint, children, className, ...props }: SelectFieldProps) {
  const generatedId = useId();
  const inputId = props.id ?? generatedId;
  const hintId = `${inputId}-hint`;
  const errorId = `${inputId}-error`;

  return (
    <FieldShell error={error} errorId={errorId} hint={hint} hintId={hintId} inputId={inputId} label={label}>
      <select
        aria-describedby={`${hintId} ${errorId}`}
        aria-invalid={error === undefined ? undefined : true}
        className={cx(inputClassName, className)}
        {...props}
        id={inputId}
      >
        {children}
      </select>
    </FieldShell>
  );
}

const inputClassName =
  "app-region-no-drag w-full rounded-[var(--radius-m)] border border-[var(--color-outline)] bg-[var(--color-island-1)] px-[14px] py-3 text-[var(--color-on-island)]";

function normalizeErrors(error: FieldError | undefined): readonly string[] {
  if (error === undefined) {
    return [];
  }
  if (typeof error === "string") {
    return error.length > 0 ? [error] : [];
  }
  return error.filter((message) => message.length > 0);
}
