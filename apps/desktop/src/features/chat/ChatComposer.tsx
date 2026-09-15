import { ArrowUp, Square } from "lucide-react";
import { useLayoutEffect, useRef, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { errorMessage } from "@/api";
import {
  ErrorState,
  IconTooltipButton,
  Island,
  PeekingSurface,
  Spinner,
  cx,
  fieldInputClassName,
} from "@/ui";
import { ComposerPendingSheet } from "./ComposerPendingSheet";
import { useComposerSurface } from "./ChatComposerSurface";
import type { useChatComposer } from "./useChatComposer";
import "./chatComposer.css";

export type ChatComposerProps = Readonly<{
  settingsChip?: ReactNode;
  availableHeight: number | null;
  onHeightChange(height: number): void;
}>;

export function ChatComposer({ settingsChip, availableHeight, onHeightChange }: ChatComposerProps) {
  const { t } = useTranslation();
  const { composer, activity, connected, stoppable, onEditorKeyDown } = useComposerSurface();
  const root = useRef<HTMLDivElement>(null);
  const editor = useRef<HTMLTextAreaElement>(null);
  const pickerOpen = composer.suggestions.length > 0;
  useLayoutEffect(() => {
    const element = editor.current;
    if (element === null) return;
    function resize() {
      if (element === null) return;
      element.style.height = "0px";
      element.style.height = `${element.scrollHeight.toString()}px`;
    }
    resize();
    const observer = new ResizeObserver(resize);
    const parent = element.parentElement;
    if (parent !== null) observer.observe(parent);
    return () => {
      observer.disconnect();
    };
  }, [composer.text, composer.draft.kind, availableHeight]);
  useLayoutEffect(() => {
    const element = root.current;
    if (element === null) return;
    let previous: number | null = null;
    function measure() {
      if (element === null) return;
      const height = element.getBoundingClientRect().height;
      if (height === previous) return;
      previous = height;
      onHeightChange(height);
    }
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => {
      observer.disconnect();
    };
  }, [onHeightChange, composer.draft.kind]);
  if (composer.draft.kind === "failed")
    return (
      <div className="chat-composer" ref={root}>
        <ErrorState
          title={t("states.error")}
          body={errorMessage(composer.draft.error)}
          onRetry={composer.retryDraft}
          retryLabel={t("app.retry")}
        />
      </div>
    );
  return (
    <div className="chat-composer" ref={root}>
      {(pickerOpen || composer.pending.items.length > 0) && (
        <PeekingSurface>
          <ComposerPendingSheet pending={composer.pending} visible={!pickerOpen} disconnected={!connected} />
          {pickerOpen && <ComposerSuggestions composer={composer} />}
        </PeekingSurface>
      )}
      <Island
        className="chat-composer-input"
        style={availableHeight === null ? undefined : { maxHeight: availableHeight / 3 }}
        unpadded
      >
        <textarea
          ref={editor}
          className={cx(fieldInputClassName, "chat-composer-editor")}
          rows={1}
          value={composer.text}
          onChange={(event) => {
            composer.edit(event.target.value);
          }}
          onKeyDown={onEditorKeyDown}
          placeholder={
            stoppable && activity?.queueAccepting
              ? t("chatComposer.queuePlaceholder")
              : t("chatComposer.placeholder")
          }
        />
        <ComposerControls
          composer={composer}
          settingsChip={settingsChip}
          connected={connected}
          stoppable={stoppable}
        />
      </Island>
    </div>
  );
}

type Composer = ReturnType<typeof useChatComposer>;

function ComposerSuggestions({ composer }: Readonly<{ composer: Composer }>) {
  return (
    <div className="chat-composer-sheet">
      {composer.suggestions.map((command) => (
        <button
          key={command.token}
          type="button"
          className={cx(
            "chat-composer-command",
            composer.selectedCommand?.token === command.token && "bg-[var(--color-island-2)]",
          )}
          onMouseDown={(event) => {
            event.preventDefault();
          }}
          onClick={() => {
            composer.edit(`${command.token} `);
          }}
        >
          <span className="font-mono">{command.token}</span>
          {command.description !== null && (
            <span className="line-clamp-1 text-[var(--color-muted)]">{command.description}</span>
          )}
          {command.preview !== null && (
            <span className="line-clamp-2 text-[var(--color-muted)]">{command.preview}</span>
          )}
        </button>
      ))}
    </div>
  );
}

function composerSendLabel(
  composer: Composer,
  connected: boolean,
  t: ReturnType<typeof useTranslation>["t"],
) {
  return !connected
    ? t("common.readOnly")
    : composer.draft.kind === "loading"
      ? t("chatComposer.loadingDraft")
      : composer.submission.kind === "loading"
        ? t("chatComposer.loadingSettings")
        : composer.submission.kind === "failed"
          ? errorMessage(composer.submission.error)
          : !composer.canSubmit
            ? t("chatComposer.empty")
            : t("chatComposer.send");
}

function ComposerControls({
  composer,
  settingsChip,
  connected,
  stoppable,
}: Readonly<{
  composer: Composer;
  settingsChip: ReactNode;
  connected: boolean;
  stoppable: boolean;
}>) {
  const { t } = useTranslation();
  return (
    <div className="chat-composer-controls">
      <div className="min-w-0 flex-1">{settingsChip}</div>
      {stoppable && (
        <IconTooltipButton
          label={connected ? t("chatComposer.stop") : t("common.readOnly")}
          disabled={!connected}
          onClick={() => {
            composer.pending.stop();
          }}
          size="icon-sm"
        >
          {composer.pending.stopPending ? <Spinner size="sm" /> : <Square size={15} />}
        </IconTooltipButton>
      )}
      <IconTooltipButton
        label={composerSendLabel(composer, connected, t)}
        disabled={!connected || !composer.canSubmit}
        variant="primary"
        onClick={() => {
          composer.submit("send");
        }}
      >
        {composer.inputPending || composer.draft.kind === "loading" ? (
          <Spinner size="sm" className="text-[var(--color-on-primary)]" />
        ) : (
          <ArrowUp size={18} />
        )}
      </IconTooltipButton>
    </div>
  );
}
