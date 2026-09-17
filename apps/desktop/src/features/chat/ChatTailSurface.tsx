import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ArrowDown, Loader2 } from "lucide-react";

import { errorMessage } from "@/api";
import { useChatRuntimeOwner, useChatRuntimeSnapshot, useStatusController } from "@/app-facade";
import { TranscriptWindowView, type TranscriptWindowViewProps } from "@/shared/transcript-window";
import { ErrorState, IconTooltipButton, LoadingState, useOpacityExit } from "@/ui";

import { ChatShell, type ChatShellProps } from "./ChatShell";
import "./chatTail.css";

export type ChatTailSurfaceProps = Omit<ChatShellProps, "content" | "onComposerHeightChange"> &
  Pick<TranscriptWindowViewProps, "slots" | "estimateSize">;

/** Mount inside the selected Session's runtime provider; row slots remain owned by the composition. */
export function ChatTailSurface({ slots, estimateSize, ...shell }: ChatTailSurfaceProps) {
  return (
    <ChatShell {...shell} content={() => <ChatTranscriptTail slots={slots} estimateSize={estimateSize} />} />
  );
}

function ChatTranscriptTail({
  slots,
  estimateSize,
}: Pick<TranscriptWindowViewProps, "slots" | "estimateSize">) {
  const { t } = useTranslation();
  const owner = useChatRuntimeOwner();
  const { transcript, transcriptPresentationUpdate, jumpPending } = useChatRuntimeSnapshot();
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
  if (transcript.opening.kind === "loading") return <LoadingState title={t("chat.tail.loading")} />;
  if (transcript.opening.kind === "disposed") return null;
  if (transcript.opening.kind === "error") {
    return (
      <ErrorState
        title={t("chat.tail.failed")}
        body={errorMessage(transcript.opening.error)}
        retryLabel={t("app.retry")}
        onRetry={() => {
          owner.transcript.dispatch({ kind: "opening-retry" });
        }}
      />
    );
  }
  return (
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
