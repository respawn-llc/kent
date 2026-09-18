import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ArrowDown, Loader2 } from "lucide-react";

import { errorMessage } from "@/api";
import { useChatRuntimeOwner, useChatRuntimeSnapshot, useStatusController } from "@/app-facade";
import { TranscriptWindowView, type TranscriptWindowViewProps } from "@/shared/transcript-window";
import { ErrorState, IconTooltipButton, LoadingState, useOpacityExit } from "@/ui";

import "./chatTail.css";
import { chatOperationFailureMessage } from "./chatSettingsPresentation";

export function ChatTranscriptTail({
  slots,
  estimateSize,
  openingVisible = true,
}: Pick<TranscriptWindowViewProps, "slots" | "estimateSize"> & Readonly<{ openingVisible?: boolean }>) {
  const { t } = useTranslation();
  const owner = useChatRuntimeOwner();
  const { transcript, transcriptPresentationUpdate, jumpPending, observation } = useChatRuntimeSnapshot();
  const status = useStatusController();
  const [scrollRequest, setScrollRequest] = useState<symbol | null>(null);
  const jump = async () => {
    const outcome = await owner.transcript.jumpToLatest();
    switch (outcome.kind) {
      case "tail-ready":
        setScrollRequest(Symbol("jump to latest"));
        break;
      case "failed":
        status.push({
          id: crypto.randomUUID(),
          title: t("chat.tail.failed"),
          body: errorMessage(outcome.error),
          tone: "danger",
        });
        break;
      case "destination-ended":
        break;
    }
  };
  if (transcript.opening.kind === "disposed") return null;
  const failure =
    transcript.opening.kind === "error"
      ? transcript.opening.error
      : observation.kind === "error"
        ? observation.error
        : null;
  const error =
    failure === null ? null : (
      <ErrorState
        fullPage={transcript.opening.kind !== "ready"}
        title={t("chat.tail.failed")}
        body={chatOperationFailureMessage(t, failure, "opening")}
        details={errorMessage(failure)}
        retryLabel={t("app.retry")}
        onRetry={() => {
          if (transcript.opening.kind === "error") owner.transcript.dispatch({ kind: "opening-retry" });
          if (observation.kind === "error") owner.retryTranscriptObservation();
        }}
      />
    );
  if (transcript.opening.kind !== "ready")
    return (
      error ?? (openingVisible ? <LoadingState title={t("chat.tail.loading")} appearanceDelayMs={0} /> : null)
    );
  return (
    <div className="flex h-full min-h-0 flex-col">
      {error}
      <div className="min-h-0 flex-1">
        <TranscriptWindowView
          snapshot={transcript}
          presentationUpdate={transcriptPresentationUpdate}
          scrollRequest={scrollRequest}
          slots={slots}
          estimateSize={estimateSize}
          loadingLabel={t("chat.tail.loading")}
          retryLabel={t("app.retry")}
          boundaryErrorMessage={errorMessage}
          onInput={(input) => {
            owner.transcript.dispatch(input);
          }}
          overlay={(following) => (
            <JumpToLatest
              visible={!following || jumpPending}
              pending={jumpPending}
              onClick={() => {
                void jump();
              }}
            />
          )}
        />
      </div>
    </div>
  );
}

function JumpToLatest({
  visible,
  pending,
  onClick,
}: Readonly<{
  visible: boolean;
  pending: boolean;
  onClick(): void;
}>) {
  const { t } = useTranslation();
  const phase = useOpacityExit(visible);
  if (phase === "hidden") return null;
  return (
    <div className="chat-tail-control" data-phase={phase}>
      <IconTooltipButton
        className="chat-tail-button island-glass"
        label={t("chat.tail.jump")}
        tooltip={pending ? t("chat.tail.loadingLatest") : t("chat.tail.jump")}
        disabled={pending || !visible}
        onClick={onClick}
      >
        {pending ? <Loader2 className="animate-spin" size={24} /> : <ArrowDown size={24} />}
      </IconTooltipButton>
    </div>
  );
}
