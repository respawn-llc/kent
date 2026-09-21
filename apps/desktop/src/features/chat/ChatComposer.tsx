import { ArrowUp, Square } from "lucide-react";
import { useLayoutEffect, useRef, type CSSProperties, type RefObject, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import {
  Button,
  ErrorState,
  IconTooltipButton,
  Island,
  PeekingSurface,
  Spinner,
  cx,
  fieldInputClassName,
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/ui";
import { ComposerPendingSheet } from "./ComposerPendingSheet";
import { ChatPromptPicker } from "./ChatPromptPicker";
import { SessionChatContext } from "./SessionChatContext";
import { useComposerSurface } from "./ChatComposerSurface";
import type { useChatComposer } from "./useChatComposer";
import type { ChatSettingsFeature } from "./useChatSettings";
import "./chatComposer.css";

export type ChatComposerProps = Readonly<{
  settings: ChatSettingsFeature;
  settingsChip?: ReactNode;
  controls?: ReactNode;
  availableHeight: number | null;
  onHeightChange(height: number): void;
  editorRef?: RefObject<HTMLTextAreaElement | null>;
}>;

export function ChatComposer({
  settings,
  settingsChip,
  controls,
  availableHeight,
  onHeightChange,
  editorRef,
}: ChatComposerProps) {
  const { t } = useTranslation();
  const { composer, activity, stoppable, promptPicker, onEditorKeyDown } = useComposerSurface();
  const root = useRef<HTMLDivElement>(null);
  const localEditor = useRef<HTMLTextAreaElement>(null);
  const editor = editorRef ?? localEditor;
  const pickerOpen = composer.pickerOpen;
  const promptVisible = promptPicker != null && promptPicker.state.current !== null;
  const heightStyle: CSSProperties & { "--chat-composer-available-height"?: string } =
    availableHeight === null ? {} : { "--chat-composer-available-height": `${availableHeight.toString()}px` };
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
  }, [composer.text, composer.draft.kind, availableHeight, editor]);
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
  const editorRegion = (
    <textarea
      ref={editor}
      autoFocus
      className={cx(fieldInputClassName, "chat-composer-editor")}
      rows={1}
      value={composer.text}
      readOnly={composer.navigationPending}
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
  );
  return (
    <div className="chat-composer" ref={root}>
      {(pickerOpen || composer.pending.items.length > 0) && (
        <PeekingSurface>
          <ComposerPendingSheet pending={composer.pending} visible={!pickerOpen} />
          {pickerOpen && <ComposerSuggestions composer={composer} />}
        </PeekingSurface>
      )}
      <Island className="chat-composer-input" style={heightStyle} unpadded>
        <div hidden={promptVisible} className={promptVisible ? "hidden" : "flex min-h-0 min-w-0 flex-col"}>
          {editorRegion}
        </div>
        {promptPicker !== null && <ChatPromptPicker picker={promptPicker} />}
        <ComposerControls
          composer={composer}
          settings={settings}
          settingsChip={settingsChip}
          stoppable={stoppable}
          controls={controls}
        />
      </Island>
    </div>
  );
}

type Composer = ReturnType<typeof useChatComposer>;

function ComposerSuggestions({ composer }: Readonly<{ composer: Composer }>) {
  const { t } = useTranslation();
  return (
    <div className="chat-composer-sheet">
      {composer.catalog?.isFetching === true && (
        <div className="flex items-center gap-[var(--space-2)] text-[var(--color-muted)]">
          <Spinner size="sm" />
          <span>{t("chatComposer.commands.loading")}</span>
        </div>
      )}
      {composer.catalog?.isError === true && (
        <ErrorState
          fullPage={false}
          title={t("chatComposer.rejections.prompt_catalog_read")}
          {...(composer.retryCatalog === undefined ? {} : { onRetry: composer.retryCatalog })}
          retryLabel={t("app.retry")}
        />
      )}
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

function composerSendLabel(composer: Composer, t: ReturnType<typeof useTranslation>["t"]) {
  return composer.navigationPending
    ? t("chat.savingDraft")
    : composer.draft.kind === "loading"
      ? t("chatComposer.loadingDraft")
      : composer.submission.kind !== "ready"
        ? t("chatComposer.loadingSettings")
        : t("chatComposer.empty");
}

function ComposerControls({
  composer,
  settings,
  settingsChip,
  stoppable,
  controls,
}: Readonly<{
  composer: Composer;
  settings: ChatSettingsFeature;
  settingsChip?: ReactNode;
  stoppable: boolean;
  controls?: ReactNode;
}>) {
  const { t } = useTranslation();
  const send = (
    <Button
      size="icon"
      aria-label={t("chatComposer.send")}
      disabled={!composer.canSubmit}
      variant="primary"
      onClick={() => {
        composer.submit("send");
      }}
    >
      {composer.inputPending || composer.navigationPending ? (
        <Spinner size="sm" className="text-[var(--color-on-primary)]" />
      ) : (
        <ArrowUp size={18} />
      )}
    </Button>
  );
  return (
    <div className="chat-composer-controls">
      <div className="chat-composer-settings">
        {settingsChip ?? ("settingsChip" in settings ? settings.settingsChip : null)}
      </div>
      {controls}
      <div className="chat-composer-actions">
        {composer.target?.kind === "session" && (
          <SessionChatContext compact={composer.compact} settings={settings} />
        )}
        {stoppable && (
          <IconTooltipButton
            label={t("chatComposer.stop")}
            onClick={() => {
              composer.pending.stop();
            }}
            size="icon-sm"
          >
            {composer.pending.stopPending ? <Spinner size="sm" /> : <Square size={15} />}
          </IconTooltipButton>
        )}
        {composer.canSubmit ? (
          send
        ) : (
          <TooltipProvider delayDuration={0}>
            <Tooltip>
              <TooltipTrigger asChild>
                <span className="inline-flex">{send}</span>
              </TooltipTrigger>
              <TooltipContent>{composerSendLabel(composer, t)}</TooltipContent>
            </Tooltip>
          </TooltipProvider>
        )}
      </div>
    </div>
  );
}
