import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ArrowDown, Loader2 } from "lucide-react";

import { errorMessage } from "@/api";
import {
  useOptionalChatRuntimeOwner,
  useOptionalChatRuntimeSnapshot,
  useStatusController,
} from "@/app-facade";
import { TranscriptWindowView, type TranscriptWindowViewProps } from "@/shared/transcript-window";
import { ErrorState, IconTooltipButton, InfiniteListBoundary, LoadingState, useOpacityExit } from "@/ui";

import "./chatTail.css";
import { chatOperationFailureMessage } from "./chatSettingsPresentation";
import { useChatMessageMotion } from "./ChatMessageMotion";

export function ChatTranscriptTail({
  slots,
  estimateSize,
  openingVisible = true,
  bottomInset,
  localMessages,
}: Pick<TranscriptWindowViewProps, "slots" | "estimateSize" | "bottomInset" | "localMessages"> &
  Readonly<{ openingVisible?: boolean }>) {
  const { t } = useTranslation();
  const owner = useOptionalChatRuntimeOwner();
  const snapshot = useOptionalChatRuntimeSnapshot();
  const transcript = snapshot?.transcript ?? null;
  const jumpPending = snapshot?.jumpPending ?? false;
  const { failure, error, showOpening } = useTranscriptFeedback(openingVisible, localMessages);
  const status = useStatusController();
  const motion = useChatMessageMotion();
  const [scrollRequest, setScrollRequest] = useState<symbol | null>(null);
  const jump = async () => {
    if (owner === null) return;
    motion.dispose();
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
  if (transcript?.opening.kind === "disposed") return null;
  return (
    <div
      className="chat-transcript flex h-full min-h-0 flex-col"
      onWheelCapture={() => {
        motion.dispose();
      }}
      onTouchMoveCapture={() => {
        motion.dispose();
      }}
      onPointerDownCapture={() => {
        motion.dispose();
      }}
      onKeyDownCapture={(event) => {
        if (["PageUp", "PageDown", "Home", "End", "ArrowUp", "ArrowDown", " "].includes(event.key))
          motion.dispose();
      }}
    >
      <div className="relative min-h-0 flex-1">
        {showOpening && <div className="absolute inset-0">{error}</div>}
        <TranscriptWindowView
          tailFailure={
            failure === null ? undefined : (
              <InfiniteListBoundary
                direction="replacement"
                state={{
                  state: "error",
                  message: errorMessage(failure),
                  retryLabel: t("app.retry"),
                  onRetry: () => {
                    owner?.retryTranscriptObservation();
                  },
                }}
              />
            )
          }
          bottomInset={bottomInset}
          snapshot={transcript}
          presentationUpdate={snapshot?.transcriptPresentationUpdate}
          localMessages={localMessages}
          scrollRequest={scrollRequest}
          slots={slots}
          estimateSize={estimateSize}
          loadingLabel={t("chat.tail.loading")}
          retryLabel={t("app.retry")}
          boundaryErrorMessage={errorMessage}
          onInput={(input) => {
            if (owner === null) throw new Error("Transcript pagination requires a Session.");
            owner.transcript.dispatch(input);
          }}
          overlay={(following) =>
            owner === null ? null : (
              <JumpToLatest
                bottomInset={bottomInset ?? 0}
                visible={!following || jumpPending}
                pending={jumpPending}
                onClick={() => {
                  void jump();
                }}
              />
            )
          }
        />
      </div>
    </div>
  );
}

function useTranscriptFeedback(
  openingVisible: boolean,
  localMessages: TranscriptWindowViewProps["localMessages"],
) {
  const { t } = useTranslation();
  const owner = useOptionalChatRuntimeOwner();
  const snapshot = useOptionalChatRuntimeSnapshot();
  const { transcript, observation } = feedbackSources(snapshot);
  const retry = () => {
    if (transcript?.opening.kind === "error") owner?.transcript.dispatch({ kind: "opening-retry" });
    if (observation?.kind === "error") owner?.retryTranscriptObservation();
  };
  const failure =
    transcript?.opening.kind === "error"
      ? transcript.opening.error
      : observation?.kind === "error"
        ? observation.error
        : null;
  const error =
    failure === null ? null : (
      <ErrorState
        fullPage={transcript?.opening.kind !== "ready"}
        title={t("chat.tail.failed")}
        body={chatOperationFailureMessage(t, failure, "opening")}
        details={errorMessage(failure)}
        retryLabel={t("app.retry")}
        onRetry={retry}
      />
    );
  return {
    failure,
    error:
      error ??
      (openingVisible ? <LoadingState title={t("chat.tail.loading")} appearanceDelayMs={0} /> : null),
    showOpening: openingNeedsFeedback(transcript, localMessages),
  };
}

function feedbackSources(snapshot: ReturnType<typeof useOptionalChatRuntimeSnapshot>) {
  return { transcript: snapshot?.transcript ?? null, observation: snapshot?.observation ?? null };
}

function openingNeedsFeedback(
  transcript: TranscriptWindowViewProps["snapshot"],
  localMessages: TranscriptWindowViewProps["localMessages"],
) {
  return transcript !== null && transcript.opening.kind !== "ready" && (localMessages?.length ?? 0) === 0;
}

function JumpToLatest({
  visible,
  pending,
  onClick,
  bottomInset,
}: Readonly<{
  visible: boolean;
  pending: boolean;
  onClick(): void;
  bottomInset: number;
}>) {
  const { t } = useTranslation();
  const phase = useOpacityExit(visible);
  if (phase === "hidden") return null;
  return (
    <div className="chat-tail-control" data-phase={phase} style={{ marginBottom: bottomInset }}>
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
