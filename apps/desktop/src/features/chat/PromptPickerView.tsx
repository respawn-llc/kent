import { useLayoutEffect, useRef, type KeyboardEvent } from "react";
import { ChevronLeft, ChevronRight, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { PendingPrompt } from "@/api";
import {
  GrowingTextArea,
  IconTooltipButton,
  PromptAccessTargets,
  PromptOptionRow,
  RadioGroup,
  ScrollRegion,
  Spinner,
  StaticMarkdown,
} from "@/ui";
import { pickerBatch, type PickerAction, type PickerState } from "./promptPickerState";
import { pickerOptions, sameSelection } from "./promptPickerPresentation";
import { promptPickerKeyboard } from "./promptPickerKeyboard";
import "./PromptPickerView.css";

export function PromptPickerView({
  prompts,
  state,
  isPending,
  dispatch,
}: Readonly<{
  prompts: readonly PendingPrompt[];
  state: PickerState;
  isPending: boolean;
  dispatch(action: PickerAction, focusField: () => void): void;
}>) {
  const { t } = useTranslation();
  const answerArea = useRef<HTMLDivElement | null>(null);
  const field = useRef<HTMLTextAreaElement | null>(null);
  useLayoutEffect(() => {
    answerArea.current?.focus();
  }, []);
  const batch = pickerBatch(prompts);
  const index = batch.findIndex((prompt) => prompt.toolCallID === state.current);
  const prompt = batch[index];
  const draft = state.current === null ? undefined : state.drafts.get(state.current);
  if (prompt === undefined || draft === undefined) return null;
  const disabled = isPending || draft.status === "declined";
  const options = pickerOptions(prompt, t);
  const selected = options.find((option) => sameSelection(option.selection, draft.selection));
  const act = (action: PickerAction) => {
    dispatch(action, () => field.current?.focus());
  };
  const keyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    promptPickerKeyboard(event, { answerArea, field, options, selected, disabled, act });
  };
  return (
    <div
      className="chat-prompt-picker flex min-h-0 min-w-0 flex-1 flex-col gap-[var(--space-2)]"
      onKeyDownCapture={keyDown}
    >
      <ScrollRegion className="flex-1">
        <div className="grid min-w-0 gap-[var(--space-3)]">
          <div className="prompt-picker-content" key={prompt.toolCallID}>
            <PromptQuestion prompt={prompt} />
          </div>
          <div className="flex items-center gap-[var(--space-1)]">
            <IconTooltipButton
              label={t("chat.picker.previous")}
              onClick={() => {
                act({ kind: "navigate", direction: -1 });
              }}
              size="icon-sm"
            >
              <ChevronLeft size={16} />
            </IconTooltipButton>
            <span className="min-w-0 text-sm">
              {t("chat.picker.position", { current: index + 1, count: batch.length })}
            </span>
            <IconTooltipButton
              label={t("chat.picker.next")}
              onClick={() => {
                act({ kind: "navigate", direction: 1 });
              }}
              size="icon-sm"
            >
              <ChevronRight size={16} />
            </IconTooltipButton>
            <div className="ml-auto flex items-center gap-[var(--space-2)]">
              {isPending ? <Spinner size="sm" /> : null}
              <IconTooltipButton
                disabled={disabled}
                label={t("chat.picker.decline")}
                tooltip={t("chat.picker.declineShortcut")}
                onClick={() => {
                  act({ kind: "decline" });
                }}
                size="icon-sm"
                variant="danger"
              >
                <X size={14} />
              </IconTooltipButton>
            </div>
          </div>
          <div ref={answerArea} tabIndex={0} className="min-w-0 outline-none">
            <RadioGroup
              disabled={disabled}
              value={selected?.value ?? null}
              onValueChange={(value) => {
                const option = options.find((item) => item.value === value);
                if (option !== undefined) act({ kind: "select", selection: option.selection });
              }}
            >
              {options.map((option) => (
                <PromptOptionRow
                  key={option.value}
                  {...option}
                  disabled={disabled}
                  selected={option === selected}
                  appearance="card"
                  onActivate={() => {
                    act({ kind: "activate", selection: option.selection });
                  }}
                />
              ))}
            </RadioGroup>
          </div>
        </div>
      </ScrollRegion>
      <GrowingTextArea
        aria-label={t("task.commentary")}
        placeholder={t("task.answerPlaceholder")}
        ref={field}
        readOnly={disabled}
        className={disabled ? "opacity-60" : undefined}
        value={draft.commentary}
        onChange={(event) => {
          act({ kind: "commentary", text: event.target.value });
        }}
      />
    </div>
  );
}

function PromptQuestion({ prompt }: Readonly<{ prompt: PendingPrompt }>) {
  if (prompt.kind === "approval" && prompt.accessTargets.length > 0)
    return <PromptAccessTargets targets={prompt.accessTargets} />;
  return prompt.question === null ? null : <StaticMarkdown value={prompt.question} />;
}
