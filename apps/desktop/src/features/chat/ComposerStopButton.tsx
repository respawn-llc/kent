import { useAtomSet } from "@effect/atom-react";
import { useTranslation } from "react-i18next";
import { Button, IconTooltipButton } from "@/ui";
import { ComposerIcon } from "./ComposerIcon";
import { useComposerSurface } from "./ChatComposerSurface";
import type { useChatPromptPicker } from "./useChatPromptPicker";

export function ComposerStopButton() {
  const { t } = useTranslation();
  const { composer, promptPicker, stoppable } = useComposerSurface();
  if (promptPicker !== null && promptPicker.state.current !== null) {
    return <PromptDeclineButton picker={promptPicker} />;
  }
  return stoppable ? (
    <IconTooltipButton
      label={t("chatComposer.stop")}
      onClick={() => {
        composer.pending.stop();
      }}
      size="icon-sm"
    >
      <ComposerIcon
        kind={composer.pending.stopPending ? "loading" : "stop"}
        className="text-[var(--color-error)]"
      />
    </IconTooltipButton>
  ) : null;
}

function PromptDeclineButton({
  picker,
}: Readonly<{ picker: NonNullable<ReturnType<typeof useChatPromptPicker>> }>) {
  const { t } = useTranslation();
  const dispatch = useAtomSet(picker.dispatch);
  const declined =
    picker.state.current !== null && picker.state.drafts.get(picker.state.current)?.status === "declined";
  if (picker.request.isPending) {
    return (
      <Button disabled variant="ghost" size="icon-sm" aria-label={t("chat.picker.decline")}>
        <ComposerIcon kind="loading" className="text-[var(--color-error)]" />
      </Button>
    );
  }
  return (
    <IconTooltipButton
      label={t("chat.picker.decline")}
      tooltip={declined ? t("chat.picker.declined") : t("chat.picker.decline")}
      disabled={declined}
      onClick={() => {
        dispatch({ action: { kind: "decline" } });
      }}
      size="icon-sm"
    >
      <ComposerIcon kind="decline" className="text-[var(--color-error)]" />
    </IconTooltipButton>
  );
}
