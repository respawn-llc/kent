import { createContext, useContext, useState, type ReactNode } from "react";
import { useAtomValue } from "@effect/atom-react";
import { useTranslation } from "react-i18next";
import { useAppServices, useStatusController } from "@/app-facade";
import {
  Button,
  ControlTooltip,
  Dialog,
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
  TextArea,
} from "@/ui";
import { createSpeechRecovery, useRecoveryActions } from "./RecoveryModel";
import { speechFailureMessage } from "./failure";

const RecoveryContext = createContext<ReturnType<typeof createSpeechRecovery> | null>(null);

export function SpeechRecoveryProvider({ children }: Readonly<{ children: ReactNode }>) {
  const services = useAppServices();
  const { t } = useTranslation();
  const { push } = useStatusController();
  const [model] = useState(() => createSpeechRecovery({ services, t, push }));
  return (
    <RecoveryContext.Provider value={model}>
      {children}
      <SpeechRecoveryDialog model={model} />
    </RecoveryContext.Provider>
  );
}

export function useSpeechRecovery() {
  const model = useContext(RecoveryContext);
  if (model === null) throw new Error("SpeechRecoveryProvider is required.");
  return model;
}

function SpeechRecoveryDialog({ model }: Readonly<{ model: ReturnType<typeof createSpeechRecovery> }>) {
  const { t } = useTranslation();
  const state = useAtomValue(model.state);
  useRecoveryActions(model);
  const entry = state.entry;
  if (entry === null) return null;
  const copyFailed = entry.delivery?.kind === "copy-failed" ? entry.delivery : null;
  return (
    <Dialog
      open
      closeDisabled
      onClose={() => undefined}
      title={t("speech.recoveryTitle")}
      closeLabel={t("app.close")}
      chrome="title-only"
    >
      <div className="grid gap-[var(--space-3)]">
        <p className="whitespace-pre-wrap">
          {copyFailed === null ? speechFailureMessage(entry.error, t) : t("speech.copyFailed")}
        </p>
        <p>{t(entry.source.kind === "native" ? "speech.temporary" : "speech.browserTemporary")}</p>
        {entry.source.kind === "native" && (
          <code className="select-text break-all">{entry.source.original.path}</code>
        )}
        {copyFailed !== null && (
          <TextArea
            readOnly
            label={t("speech.transcript")}
            value={copyFailed.text}
            className="max-h-64 min-h-32 w-full resize-y overflow-auto"
          />
        )}
        <RecoveryControls model={model} native={entry.source.kind === "native"} />
      </div>
    </Dialog>
  );
}

function RecoveryControls({
  model,
  native,
}: Readonly<{ model: ReturnType<typeof createSpeechRecovery>; native: boolean }>) {
  const { t } = useTranslation();
  const state = useAtomValue(model.state);
  const fileAction = useAtomValue(model.fileAction);
  const actions = useRecoveryActions(model);
  const busy = state.work !== "idle" || fileAction.waiting;
  const discarding = state.work === "discarding";
  const discardDisabled = discarding || fileAction.waiting;
  return (
    <TooltipProvider>
      <div className="flex flex-wrap justify-end gap-[var(--space-2)]">
        {native && (
          <ControlTooltip label={busy ? t("speech.working") : t("speech.reveal")}>
            <Button
              disabled={busy}
              loading={fileAction.waiting}
              onClick={() => {
                actions.fileAction("reveal");
              }}
            >
              {t("speech.reveal")}
            </Button>
          </ControlTooltip>
        )}
        <ControlTooltip label={busy ? t("speech.working") : t("speech.saveCopy")}>
          <Button
            disabled={busy}
            loading={fileAction.waiting}
            onClick={() => {
              actions.fileAction("save");
            }}
          >
            {t(native ? "speech.saveCopy" : "speech.download")}
          </Button>
        </ControlTooltip>
        {state.entry?.delivery?.kind !== "cleanup-failed" && (
          <Tooltip>
            <TooltipTrigger asChild>
              <span>
                <Button
                  disabled={busy}
                  loading={state.work === "retrying"}
                  onClick={() => {
                    actions.retry();
                  }}
                >
                  {t("speech.retry")}
                </Button>
              </span>
            </TooltipTrigger>
            <TooltipContent>{busy ? t("speech.working") : t("speech.retry")}</TooltipContent>
          </Tooltip>
        )}
        <Tooltip>
          <TooltipTrigger asChild>
            <span>
              <Button
                disabled={discardDisabled}
                loading={discarding}
                variant="danger"
                onClick={() => {
                  actions.discard();
                }}
              >
                {t("speech.discard")}
              </Button>
            </span>
          </TooltipTrigger>
          <TooltipContent>{discardDisabled ? t("speech.working") : t("speech.discard")}</TooltipContent>
        </Tooltip>
      </div>
    </TooltipProvider>
  );
}
