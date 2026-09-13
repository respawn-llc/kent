import { useImperativeHandle, useLayoutEffect, useRef, type ComponentProps, type Ref } from "react";
import { cx } from "./classes";
import { fieldInputClassName } from "./fieldInputStyles";

export function GrowingTextArea({
  value,
  className,
  ref,
  ...props
}: ComponentProps<"textarea"> & Readonly<{ ref?: Ref<HTMLTextAreaElement> }>) {
  const field = useRef<HTMLTextAreaElement | null>(null);
  useImperativeHandle(ref, () => {
    if (field.current === null) throw new Error("Growing textarea is not mounted.");
    return field.current;
  }, []);
  useLayoutEffect(() => {
    const element = field.current;
    if (element === null) return;
    const measure = () => {
      element.style.height = "auto";
      element.style.height = `${String(element.scrollHeight + element.offsetHeight - element.clientHeight)}px`;
    };
    measure();
    let width: number | null = null;
    const observer = new ResizeObserver(([entry]) => {
      if (entry !== undefined && entry.contentRect.width !== width) {
        width = entry.contentRect.width;
        measure();
      }
    });
    observer.observe(element);
    return () => {
      observer.disconnect();
    };
  }, [value]);
  return (
    <textarea
      {...props}
      className={cx(
        fieldInputClassName,
        "min-h-[calc(3lh+var(--space-3)*2+2px)] max-h-[calc(7lh+var(--space-3)*2+2px)] shrink-0 resize-none overflow-y-auto",
        className,
      )}
      ref={field}
      rows={3}
      value={value}
    />
  );
}
