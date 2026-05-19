import {
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from "react";

import { FieldShell, type FieldError } from "./Field";
import { SelectOptionsList, SelectTrigger } from "./SelectFieldParts";

export type SelectFieldOption = Readonly<{
  label: ReactNode;
  textValue?: string;
  value: string;
  disabled?: boolean;
}>;

export type SelectFieldProps = Readonly<{
  label: string;
  value: string;
  options: readonly SelectFieldOption[];
  onValueChange: (value: string) => void;
  className?: string | undefined;
  error?: FieldError | undefined;
  hint?: ReactNode | undefined;
  placeholder?: string | undefined;
  disabled?: boolean | undefined;
  name?: string | undefined;
}>;

export function SelectField({
  label,
  value,
  options,
  onValueChange,
  className,
  error,
  hint,
  placeholder,
  disabled = false,
  name,
}: SelectFieldProps) {
  const inputId = useId();
  const hintId = `${inputId}-hint`;
  const errorId = `${inputId}-error`;

  return (
    <FieldShell error={error} errorId={errorId} hint={hint} hintId={hintId} inputId={inputId} label={label}>
      <SelectFieldControl
        className={className}
        disabled={disabled}
        error={error}
        errorId={errorId}
        hintId={hintId}
        inputId={inputId}
        name={name}
        onValueChange={onValueChange}
        options={options}
        placeholder={placeholder}
        value={value}
      />
    </FieldShell>
  );
}

type SelectFieldControlProps = Readonly<{
  value: string;
  options: readonly SelectFieldOption[];
  onValueChange: (value: string) => void;
  inputId: string;
  hintId: string;
  errorId: string;
  error?: FieldError | undefined;
  placeholder?: string | undefined;
  className?: string | undefined;
  disabled: boolean;
  name?: string | undefined;
}>;

function SelectFieldControl({
  value,
  options,
  onValueChange,
  inputId,
  hintId,
  errorId,
  error,
  placeholder,
  className,
  disabled,
  name,
}: SelectFieldControlProps) {
  const rootRef = useRef<HTMLDivElement | null>(null);
  const [open, setOpen] = useState(false);
  const listboxId = `${inputId}-listbox`;
  const selectedOption = useMemo(() => options.find((option) => option.value === value), [options, value]);
  const firstEnabledIndex = useMemo(() => findFirstEnabledOptionIndex(options), [options]);
  const selectedIndex = useMemo(
    () => options.findIndex((option) => option.value === value),
    [options, value],
  );
  const [activeIndex, setActiveIndex] = useState(selectedIndex >= 0 ? selectedIndex : firstEnabledIndex);
  const normalizedActiveIndex = normalizeActiveIndex(options, activeIndex, selectedIndex, firstEnabledIndex);
  const activeOption = normalizedActiveIndex >= 0 ? options[normalizedActiveIndex] : undefined;
  const activeOptionId =
    open && activeOption !== undefined ? optionId(inputId, activeOption.value) : undefined;
  const interactiveDisabled = disabled || options.length === 0;

  useEffect(() => {
    if (!open) {
      return undefined;
    }
    const closeOnPointerDown = (event: PointerEvent) => {
      const root = rootRef.current;
      if (root === null || !(event.target instanceof Node) || root.contains(event.target)) {
        return;
      }
      setOpen(false);
    };
    document.addEventListener("pointerdown", closeOnPointerDown);
    return () => {
      document.removeEventListener("pointerdown", closeOnPointerDown);
    };
  }, [open]);

  const selectOption = useCallback(
    (option: SelectFieldOption) => {
      if (option.disabled) {
        return;
      }
      onValueChange(option.value);
      setOpen(false);
    },
    [onValueChange],
  );
  const moveActive = useCallback(
    (direction: 1 | -1) => {
      setOpen(true);
      setActiveIndex((current) =>
        nextEnabledOptionIndex(
          options,
          normalizeActiveIndex(options, current, selectedIndex, firstEnabledIndex),
          direction,
        ),
      );
    },
    [firstEnabledIndex, options, selectedIndex],
  );
  const toggleOpen = useCallback(() => {
    if (open) {
      setOpen(false);
      return;
    }
    setActiveIndex(selectedIndex >= 0 ? selectedIndex : firstEnabledIndex);
    setOpen(true);
  }, [firstEnabledIndex, open, selectedIndex]);
  const handleTriggerKeyDown = useCallback(
    (event: KeyboardEvent<HTMLButtonElement>) => {
      handleSelectKeyDown({
        activeOption,
        disabled: interactiveDisabled,
        event,
        moveActive,
        open,
        openWithReset: () => {
          setActiveIndex(selectedIndex >= 0 ? selectedIndex : firstEnabledIndex);
          setOpen(true);
        },
        selectOption,
        setOpen,
      });
    },
    [activeOption, firstEnabledIndex, interactiveDisabled, moveActive, open, selectOption, selectedIndex],
  );

  return (
    <div className="relative" ref={rootRef}>
      <SelectTrigger
        activeOptionId={activeOptionId}
        className={className}
        disabled={interactiveDisabled}
        error={error}
        errorId={errorId}
        hintId={hintId}
        inputId={inputId}
        listboxId={listboxId}
        onClick={toggleOpen}
        onKeyDown={handleTriggerKeyDown}
        open={open}
        placeholder={placeholder}
        selectedOption={selectedOption}
      />
      {name === undefined ? null : <input name={name} type="hidden" value={value} />}
      {open ? (
        <SelectOptionsList
          activeIndex={normalizedActiveIndex}
          inputId={inputId}
          listboxId={listboxId}
          onActiveIndexChange={setActiveIndex}
          onSelect={selectOption}
          options={options}
          value={value}
        />
      ) : null}
    </div>
  );
}

type SelectKeyDownState = Readonly<{
  event: KeyboardEvent<HTMLButtonElement>;
  disabled: boolean;
  open: boolean;
  activeOption: SelectFieldOption | undefined;
  moveActive: (direction: 1 | -1) => void;
  openWithReset: () => void;
  selectOption: (option: SelectFieldOption) => void;
  setOpen: (open: boolean) => void;
}>;

function handleSelectKeyDown({
  event,
  disabled,
  open,
  activeOption,
  moveActive,
  openWithReset,
  selectOption,
  setOpen,
}: SelectKeyDownState) {
  if (disabled) {
    return;
  }
  if (event.key === "Escape") {
    setOpen(false);
    return;
  }
  if (event.key === "ArrowDown") {
    event.preventDefault();
    moveActive(1);
    return;
  }
  if (event.key === "ArrowUp") {
    event.preventDefault();
    moveActive(-1);
    return;
  }
  if (event.key === "Enter" || event.key === " ") {
    event.preventDefault();
    if (!open) {
      openWithReset();
      return;
    }
    if (activeOption !== undefined) {
      selectOption(activeOption);
    }
  }
}

function findFirstEnabledOptionIndex(options: readonly SelectFieldOption[]): number {
  return options.findIndex((option) => option.disabled !== true);
}

function normalizeActiveIndex(
  options: readonly SelectFieldOption[],
  currentIndex: number,
  selectedIndex: number,
  firstEnabledIndex: number,
): number {
  if (currentIndex >= 0 && currentIndex < options.length && options[currentIndex]?.disabled !== true) {
    return currentIndex;
  }
  if (selectedIndex >= 0 && options[selectedIndex]?.disabled !== true) {
    return selectedIndex;
  }
  return firstEnabledIndex;
}

function nextEnabledOptionIndex(
  options: readonly SelectFieldOption[],
  currentIndex: number,
  direction: 1 | -1,
): number {
  const enabledIndexes = options
    .map((option, index) => (option.disabled === true ? -1 : index))
    .filter((index) => index >= 0);
  if (enabledIndexes.length === 0) {
    return -1;
  }
  const currentEnabledIndex = enabledIndexes.findIndex((index) => index === currentIndex);
  if (currentEnabledIndex < 0) {
    return enabledIndexes[direction > 0 ? 0 : enabledIndexes.length - 1] ?? -1;
  }
  const nextIndex = (currentEnabledIndex + direction + enabledIndexes.length) % enabledIndexes.length;
  return enabledIndexes[nextIndex] ?? -1;
}

function optionId(inputId: string, value: string): string {
  return `${inputId}-option-${value}`;
}
