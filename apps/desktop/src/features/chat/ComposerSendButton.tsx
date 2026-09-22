import { useAtomSet } from "@effect/atom-react";
import { useTranslation } from "react-i18next";
import { IconTooltipButton } from "@/ui";
import { ComposerIcon } from "./ComposerIcon";
import { useComposerSurface } from "./ChatComposerSurface";
import { canConfirmPicker } from "./promptPickerState";
import type { useChatPromptPicker } from "./useChatPromptPicker";

export function ComposerSendButton() {
  const { t } = useTranslation();
  const { composer, promptPicker } = useComposerSurface();
  if (promptPicker !== null && promptPicker.state.current !== null) {
    return <PromptSendButton picker={promptPicker} />;
  }
  const reason = composer.navigationPending
    ? t("chat.savingDraft")
    : composer.draft.kind === "loading"
      ? t("chatComposer.loadingDraft")
      : composer.submission.kind !== "ready"
        ? t("chatComposer.loadingSettings")
        : t("chatComposer.empty");
  return (
    <SendButton
      enabled={composer.canSubmit}
      pending={composer.inputPending || composer.navigationPending}
      tooltip={composer.canSubmit ? t("chatComposer.send") : reason}
      onClick={() => {
        composer.submit("send");
      }}
    />
  );
}

function PromptSendButton({
  picker,
}: Readonly<{ picker: NonNullable<ReturnType<typeof useChatPromptPicker>> }>) {
  const { t } = useTranslation();
  const dispatch = useAtomSet(picker.dispatch);
  return (
    <SendButton
      enabled={!picker.request.isPending && canConfirmPicker(picker.state)}
      pending={picker.request.isPending}
      tooltip={t("chatComposer.send")}
      onClick={() => {
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
}: Readonly<{
  enabled: boolean;
  pending: boolean;
  tooltip: string;
  onClick(): void;
}>) {
  const { t } = useTranslation();
  return (
    <IconTooltipButton
      size="icon"
      label={t("chatComposer.send")}
      tooltip={tooltip}
      disabled={!enabled}
      variant="primary"
      onClick={onClick}
    >
      <ComposerIcon kind={pending ? "loading" : "send"} className="text-[var(--color-on-primary)]" />
    </IconTooltipButton>
  );
}
