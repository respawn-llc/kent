import {
  createContext,
  useContext,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
  type RefObject,
} from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useAtomMount, useAtomSet, useAtomValue } from "@effect/atom-react";
import { useTranslation } from "react-i18next";
import { errorMessage } from "@/api";
import { useAppServices, useStatusController } from "@/app-facade";
import { ErrorState, RecordingControl } from "@/ui";
import { createSpeechDestination, useSpeechActions, type SpeechDestination } from "./model";
import { createSpeechTarget, type SpeechDraft } from "./insertion";
import { useSpeechRecovery } from "./RecoveryProvider";
import { speechFailureMessage } from "./failure";

const DestinationContext = createContext<Readonly<{
  model: SpeechDestination;
  isOpen(): boolean;
}> | null>(null);

export function SpeechDestinationProvider({ children }: Readonly<{ children: ReactNode }>) {
  const services = useAppServices();
  const client = useQueryClient();
  const recovery = useSpeechRecovery();
  const offer = useAtomSet(recovery.offer);
  const { t } = useTranslation();
  const { push } = useStatusController();
  const [destination] = useState(() => {
    const report = (error: Error) => {
      push({
        id: "speech-failed",
        title: t("speech.failed"),
        body: speechFailureMessage(error, t),
        tone: "danger",
      });
    };
    return createSpeechDestination({
      services,
      client,
      deliver: (source, result, target) => recovery.deliver({ source, result, target }),
      recover: (source, error, target) => {
        offer({ source, error, target });
      },
      report,
      onLimit: () => {
        push({ id: "speech-limit", title: t("speech.limit"), body: t("speech.limitBody"), tone: "info" });
        void services.speech.limitSound().catch(report);
      },
    });
  });
  useAtomMount(destination.start);
  const state = useAtomValue(destination.state);
  const refresh = useAtomSet(destination.refresh);
  const open = useRef(true);
  useLayoutEffect(() => {
    open.current = true;
    return () => {
      open.current = false;
    };
  }, []);
  const [context] = useState(() => ({ model: destination, isOpen: () => open.current }));
  return (
    <DestinationContext.Provider value={context}>
      {state.availability.isError && (
        <ErrorState
          fullPage={false}
          title={t("speech.failed")}
          body={errorMessage(state.availability.error)}
          retryLabel={t("app.retry")}
          onRetry={refresh}
        />
      )}
      {children}
    </DestinationContext.Provider>
  );
}

type FieldBinding = Readonly<{
  identity?: string;
  value: string;
  onChange(text: string): void;
  readOnly?: boolean;
  editorRef?: RefObject<HTMLTextAreaElement | null>;
  captureDraft?: () => SpeechDraft;
}>;

export function useSpeechDestinationBusy() {
  const destination = useContext(DestinationContext);
  if (destination === null) throw new Error("SpeechDestinationProvider is required.");
  return useAtomValue(destination.model.state).busy;
}

export function useSpeechField(binding: FieldBinding) {
  const destination = useContext(DestinationContext);
  if (destination === null) throw new Error("SpeechDestinationProvider is required.");
  const { t } = useTranslation();
  const [localIdentity] = useState(() => Symbol("speech-field"));
  const field = binding.identity ?? localIdentity;
  const localEditor = useRef<HTMLTextAreaElement>(null);
  const editorRef = binding.editorRef ?? localEditor;
  const owner = useRef<FieldBinding | null>(binding);
  useLayoutEffect(() => {
    owner.current = binding;
  });
  useLayoutEffect(
    () => () => {
      owner.current = null;
    },
    [],
  );
  const state = useAtomValue(destination.model.state);
  const own = state.origin === field;
  const phase = own ? state.phase : "idle";
  const actions = useSpeechActions(destination.model, field, () => {
    const captured = owner.current?.captureDraft?.();
    return createSpeechTarget({
      read: () =>
        !destination.isOpen()
          ? null
          : captured === undefined
            ? (owner.current?.value ?? null)
            : captured.read(),
      write: (text) => {
        if (captured === undefined) owner.current?.onChange(text);
        else captured.write(text);
      },
      editor: () => (captured === undefined ? editorRef.current : captured.editor()),
    });
  });
  const control = fieldControlHidden(binding.readOnly, state) ? undefined : (
    <SpeechFieldControl state={state} own={own} phase={phase} actions={actions} />
  );
  return { control, busy: own && state.busy, busyReason: t("speech.busy"), editorRef };
}

type DestinationState = ReturnType<ReturnType<typeof createSpeechDestination>["state"]["read"]>;

function fieldControlHidden(readOnly: boolean | undefined, state: DestinationState) {
  return readOnly === true || (state.available === null && !state.busy);
}

function SpeechFieldControl({
  state,
  own,
  phase,
  actions,
}: Readonly<{
  state: DestinationState;
  own: boolean;
  phase: DestinationState["phase"];
  actions: ReturnType<typeof useSpeechActions>;
}>) {
  const { t } = useTranslation();
  const labels = {
    idle: "speech.record",
    recording: "speech.stop",
    preparing: "speech.preparing",
    transcribing: "speech.transcribing",
  } as const;
  const label = t(labels[phase]);
  const siblingBusy = state.busy && !own;
  return (
    <RecordingControl
      phase={phase}
      recording={own ? state.recording : null}
      disabled={siblingBusy || phase === "preparing" || phase === "transcribing"}
      label={label}
      tooltip={siblingBusy ? t("speech.sibling") : label}
      onStart={actions.start}
      onStop={actions.stop}
    />
  );
}
