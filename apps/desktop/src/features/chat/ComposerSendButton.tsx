import { useAtomSet } from "@effect/atom-react";
import type { MouseEventHandler } from "react";
import { useTranslation } from "react-i18next";
import { IconTooltipButton } from "@/ui";
import { ComposerIcon } from "./ComposerIcon";
import { useComposerSurface } from "./ChatComposerSurface";
import { canConfirmPicker } from "./promptPickerState";
import type { useChatPromptPicker } from "./useChatPromptPicker";

export function ComposerSendButton() {
  const { composer, promptPicker, speech } = useComposerSurface();
  if (promptPicker !== null && promptPicker.state.current !== null) {
    return (
      <PromptSendButton
        picker={promptPicker}
        restricted={composer.interactionRestricted || (speech.busy && speech.phase !== "recording")}
      />
    );
  }
  return <OrdinarySendButton />;
}

function OrdinarySendButton() {
  const { t } = useTranslation();
  const { composer, speech, queueHeld, onSendClick } = useComposerSurface();
  const label = t(queueHeld ? "chatComposer.queue" : "chatComposer.send");
  const reason = composer.heldReadonly
    ? t("chatComposer.awaitingConfirmation")
    : !composer.canPrepare
      ? t("chatComposer.rejections.pending_work_capacity")
      : composer.interactionRestricted ||
          composer.draft.kind === "loading" ||
          composer.submission.kind !== "ready"
        ? label
        : t("chatComposer.empty");
  return (
    <SendButton
      enabled={composer.canSubmit}
      pending={composer.inputPending || composer.interactionRestricted}
      intent={queueHeld ? "queue" : "send"}
      tooltip={speech.busy && speech.phase !== "recording" ? label : composer.canSubmit ? label : reason}
      onClick={onSendClick}
    />
  );
}

function PromptSendButton({
  picker,
  restricted,
}: Readonly<{
  picker: NonNullable<ReturnType<typeof useChatPromptPicker>>;
  restricted: boolean;
}>) {
  const { t } = useTranslation();
  const dispatch = useAtomSet(picker.dispatch);
  const { speech } = useComposerSurface();
  return (
    <SendButton
      enabled={!restricted && !picker.request.isPending && canConfirmPicker(picker.state)}
      pending={restricted || picker.request.isPending}
      tooltip={t("chatComposer.send")}
      onClick={async () => {
        if (speech.busy && !(await speech.finish())) return;
        dispatch({ action: { kind: "confirm" } });
      }}
    />
  );
}

function SendButton({
  enabled,
  pending,
  tooltip,
  onClick,
  intent = "send",
}: Readonly<{
  enabled: boolean;
  pending: boolean;
  tooltip: string;
  onClick: MouseEventHandler<HTMLButtonElement>;
  intent?: "send" | "queue";
}>) {
  const { t } = useTranslation();
  return (
    <IconTooltipButton
      size="icon"
      label={t(intent === "queue" ? "chatComposer.queue" : "chatComposer.send")}
      tooltip={tooltip}
      disabled={!enabled}
      variant="primary"
      onClick={onClick}
    >
      {pending ? (
        <ComposerIcon kind="loading" className="text-[var(--color-on-primary)]" />
      ) : (
        <span className="relative grid size-5 place-items-center">
          {(["send", "queue"] as const).map((kind) => (
            <span
              key={kind}
              className="absolute grid place-items-center transition-opacity duration-[var(--motion-fast)]"
              style={{ opacity: intent === kind ? 1 : 0 }}
            >
              <ComposerIcon kind={kind} className="text-[var(--color-on-primary)]" />
            </span>
          ))}
        </span>
      )}
    </IconTooltipButton>
  );
}
