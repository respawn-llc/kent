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
        reason={speech.busy && speech.phase !== "recording" ? speech.busyReason : undefined}
      />
    );
  }
  return <OrdinarySendButton />;
}

function OrdinarySendButton() {
  const { t } = useTranslation();
  const { composer, speech, queueHeld, onSendClick } = useComposerSurface();
  const reason = composer.heldReadonly
    ? t("chatComposer.awaitingConfirmation")
    : !composer.canPrepare
      ? t("chatComposer.rejections.pending_work_capacity")
      : composer.interactionRestricted
        ? t("chat.navigationPending")
        : composer.draft.kind === "loading"
          ? t("chatComposer.loadingDraft")
          : composer.submission.kind !== "ready"
            ? t("chatComposer.loadingSettings")
            : t("chatComposer.empty");
  return (
    <SendButton
      enabled={composer.canSubmit}
      pending={composer.inputPending || composer.interactionRestricted}
      intent={queueHeld ? "queue" : "send"}
      tooltip={
        speech.busy && speech.phase !== "recording"
          ? speech.busyReason
          : composer.canSubmit
            ? t(queueHeld ? "chatComposer.queue" : "chatComposer.send")
            : reason
      }
      onClick={onSendClick}
    />
  );
}

function PromptSendButton({
  picker,
  restricted,
  reason,
}: Readonly<{
  picker: NonNullable<ReturnType<typeof useChatPromptPicker>>;
  restricted: boolean;
  reason?: string | undefined;
}>) {
  const { t } = useTranslation();
  const dispatch = useAtomSet(picker.dispatch);
  const { speech } = useComposerSurface();
  return (
    <SendButton
      enabled={!restricted && !picker.request.isPending && canConfirmPicker(picker.state)}
      pending={restricted || picker.request.isPending}
      tooltip={restricted ? (reason ?? t("chat.navigationPending")) : t("chatComposer.send")}
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
