import { Check, ChevronDown } from "lucide-react";
import type { KeyboardEvent } from "react";

import { cx } from "./classes";
import { fieldInputClassName, type FieldError } from "./Field";
import type { SelectFieldOption } from "./SelectField";

export type SelectTriggerProps = Readonly<{
  inputId: string;
  listboxId: string;
  hintId: string;
  errorId: string;
  error?: FieldError | undefined;
  selectedOption: SelectFieldOption | undefined;
  placeholder?: string | undefined;
  open: boolean;
  disabled: boolean;
  activeOptionId: string | undefined;
  className?: string | undefined;
  onClick: () => void;
  onKeyDown: (event: KeyboardEvent<HTMLButtonElement>) => void;
}>;

export function SelectTrigger({
  inputId,
  listboxId,
  hintId,
  errorId,
  error,
  selectedOption,
  placeholder,
  open,
  disabled,
  activeOptionId,
  className,
  onClick,
  onKeyDown,
}: SelectTriggerProps) {
  return (
    <button
      aria-activedescendant={activeOptionId}
      aria-controls={listboxId}
      aria-describedby={`${hintId} ${errorId}`}
      aria-expanded={open}
      aria-haspopup="listbox"
      aria-invalid={error === undefined ? undefined : true}
      className={cx(
        fieldInputClassName,
        "flex items-center justify-between gap-[var(--space-2)] text-left outline-none transition-[border-color,box-shadow] focus-visible:border-[var(--color-primary)] focus-visible:ring-[3px] focus-visible:ring-[color-mix(in_srgb,var(--color-primary)_35%,transparent)] disabled:cursor-not-allowed disabled:opacity-55",
        selectedOption === undefined ? "text-[var(--color-muted)]" : undefined,
        className,
      )}
      data-placeholder={selectedOption === undefined ? "true" : undefined}
      data-slot="select-trigger"
      disabled={disabled}
      id={inputId}
      onClick={onClick}
      onKeyDown={onKeyDown}
      role="combobox"
      type="button"
    >
      <span className="min-w-0 truncate">{selectedOption?.label ?? placeholder}</span>
      <ChevronDown
        aria-hidden="true"
        className={cx("shrink-0 text-[var(--color-muted)] transition-transform", open ? "rotate-180" : undefined)}
        size={16}
        strokeWidth={1.5}
      />
    </button>
  );
}

export type SelectOptionsListProps = Readonly<{
  inputId: string;
  listboxId: string;
  options: readonly SelectFieldOption[];
  value: string;
  activeIndex: number;
  onSelect: (option: SelectFieldOption) => void;
  onActiveIndexChange: (index: number) => void;
}>;

export function SelectOptionsList({
  inputId,
  listboxId,
  options,
  value,
  activeIndex,
  onSelect,
  onActiveIndexChange,
}: SelectOptionsListProps) {
  return (
    <div
      className="app-region-no-drag absolute left-0 right-0 top-[calc(100%+var(--space-1))] z-50 max-h-80 overflow-auto rounded-[var(--radius-m)] border border-[var(--color-outline)] bg-[var(--color-island-3)] p-[var(--space-1)] text-[var(--color-on-island)] shadow-[var(--shadow-island-1)] backdrop-blur-[20px]"
      id={listboxId}
      role="listbox"
    >
      {options.map((option, index) => (
        <SelectOption
          active={index === activeIndex}
          inputId={inputId}
          key={option.value}
          onActive={() => {
            onActiveIndexChange(index);
          }}
          onSelect={onSelect}
          option={option}
          selected={option.value === value}
        />
      ))}
    </div>
  );
}

type SelectOptionProps = Readonly<{
  inputId: string;
  option: SelectFieldOption;
  selected: boolean;
  active: boolean;
  onSelect: (option: SelectFieldOption) => void;
  onActive: () => void;
}>;

function SelectOption({ inputId, option, selected, active, onSelect, onActive }: SelectOptionProps) {
  return (
    <button
      aria-selected={selected}
      className={cx(
        "relative grid min-h-9 w-full select-none grid-cols-[1fr_auto] items-center gap-[var(--space-2)] rounded-[var(--radius-s)] px-[var(--space-2)] py-[var(--space-2)] text-left text-sm outline-none transition-colors disabled:cursor-not-allowed disabled:opacity-45",
        active ? "bg-[var(--color-island-2)]" : undefined,
      )}
      disabled={option.disabled === true}
      id={optionId(inputId, option.value)}
      onClick={() => {
        onSelect(option);
      }}
      onMouseEnter={() => {
        if (option.disabled !== true) {
          onActive();
        }
      }}
      role="option"
      tabIndex={-1}
      type="button"
    >
      <span className="min-w-0 truncate">{option.label}</span>
      {selected ? (
        <span className="text-[var(--color-primary)]">
          <Check aria-hidden="true" size={16} strokeWidth={1.7} />
        </span>
      ) : null}
    </button>
  );
}

function optionId(inputId: string, value: string): string {
  return `${inputId}-option-${value}`;
}
