import { useLayoutEffect, useRef, type ReactNode } from "react";

import { useAppServices } from "../app/useAppServices";
import { cx } from "./classes";

export type NativeDialogWindowProps = Readonly<{
  title: string;
  children: ReactNode;
  fitToContent?: boolean;
  contentMaxWidth?: string;
}>;

export function NativeDialogWindow({
  title,
  children,
  fitToContent = true,
  contentMaxWidth = "var(--content-max-width-dialog)",
}: NativeDialogWindowProps) {
  const { nativeBridge } = useAppServices();
  const shellRef = useRef<HTMLElement | null>(null);

  useLayoutEffect(() => {
    if (!fitToContent) {
      return undefined;
    }
    const shell = shellRef.current;
    if (shell === null) {
      return undefined;
    }
    let lastSize = "";
    let frame = 0;
    const fit = () => {
      const rect = shell.getBoundingClientRect();
      const width = Math.ceil(rect.width);
      const height = Math.ceil(rect.height);
      const key = `${width.toString()}x${height.toString()}`;
      if (key === lastSize || width <= 0 || height <= 0) {
        return;
      }
      lastSize = key;
      void nativeBridge.window.fitCurrentToContent({ height, width });
    };
    const scheduleFit = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(fit);
    };
    scheduleFit();
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(scheduleFit);
    observer?.observe(shell);
    return () => {
      cancelAnimationFrame(frame);
      observer?.disconnect();
    };
  }, [fitToContent, nativeBridge.window]);

  return (
    <main
      className={cx(
        "window-glass-fill grid p-[var(--native-titlebar-height)_var(--space-2)_var(--space-2)]",
        fitToContent ? "w-max" : "h-screen w-screen",
      )}
      ref={shellRef}
    >
      <div
        className="app-region-drag fixed inset-x-0 top-0 h-[var(--native-titlebar-height)]"
        data-tauri-drag-region
      />
      <section
        aria-label={title}
        aria-modal="true"
        className={cx(
          "app-region-no-drag island-glass grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-[var(--space-3)] rounded-[var(--radius-xl)] p-[var(--space-4)]",
          fitToContent ? "w-max" : "h-full",
        )}
        role="dialog"
      >
        <div
          className="mx-auto grid h-full min-h-0 w-full gap-[var(--space-3)] grid-rows-[auto_minmax(0,1fr)]"
          style={{ maxWidth: contentMaxWidth }}
        >
          <header className="min-w-0">
            <h1 className="m-0 text-[1.15rem] font-bold">{title}</h1>
          </header>
          <div className="min-h-0 overflow-auto hide-scrollbar">{children}</div>
        </div>
      </section>
    </main>
  );
}
