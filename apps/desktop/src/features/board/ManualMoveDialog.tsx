import { createContext, useContext, useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import type { TaskMovePreviewChoice, TaskMovePreviewResponse } from "@/api";
import { useTextFieldSubmitShortcut } from "@/app-facade";
import { Button, ControlTooltip, Dialog, RadioGroup, RadioGroupItem, TextArea } from "@/ui";
import { SpeechDestinationProvider, useSpeechDestinationBusy, useSpeechField } from "@/shared/speech-input";

type ManualMoveValues = Readonly<Record<string, Readonly<Record<string, string>>>>;
const MoveDraftContext = createContext<
  | ((
      node: string,
      output: string,
    ) => {
      read(): string | null;
      write(text: string): void;
    })
  | null
>(null);

export type ManualMoveDialogSubmit = Readonly<{
  transitionKey?: string | undefined;
  values?: ManualMoveValues | undefined;
}>;

export function ManualMoveDialog(props: Parameters<typeof ManualMoveDestination>[0]) {
  return (
    <SpeechDestinationProvider>
      <ManualMoveDestination {...props} />
    </SpeechDestinationProvider>
  );
}

function ManualMoveDestination({
  preview,
  onCancel,
  onSubmit,
}: Readonly<{
  preview: TaskMovePreviewResponse | null;
  onCancel(): void;
  onSubmit(input: ManualMoveDialogSubmit): void;
}>) {
  const { t } = useTranslation();
  const speechBusy = useSpeechDestinationBusy();
  const choices = manualMoveChoices(preview);
  const [selectedTransitionKey, setSelectedTransitionKey] = useState<string | null>(
    choices.length === 1 ? (choices[0]?.transitionKey ?? null) : null,
  );
  const [phase, setPhase] = useState<"choices" | "details">(choices.length > 1 ? "choices" : "details");
  const selectedChoice = manualMoveSelectedChoice(choices, selectedTransitionKey);
  const requiredValues = manualMoveRequiredValues(selectedChoice);
  const [values, setValues] = useState<ManualMoveValues>(() => initialValues(requiredValues));
  const owner = useRef({ values, selectedTransitionKey });
  useLayoutEffect(() => {
    owner.current = { values, selectedTransitionKey };
  });
  const captureDraft = (node: string, output: string) => {
    const transition = owner.current.selectedTransitionKey;
    return {
      read: () =>
        owner.current.selectedTransitionKey === transition
          ? (owner.current.values[node]?.[output] ?? "")
          : null,
      write: (text: string) => {
        if (owner.current.selectedTransitionKey === transition)
          setValues((current) => setValue(current, node, output, text));
      },
    };
  };
  const canSubmit = !speechBusy && manualMoveCanSubmit(requiredValues, values);

  const title = t("board.manualMoveTitle");
  const canAdvance = manualMoveCanAdvance(preview, selectedChoice);
  const submit = () => {
    onSubmit(manualMoveSubmit(selectedChoice?.transitionKey, values));
  };

  return (
    <Dialog closeLabel={t("app.close")} onClose={onCancel} open={preview !== null} title={title}>
      {preview === null ? null : (
        <MoveDraftContext.Provider value={captureDraft}>
          <ManualMoveDialogContent
            canAdvance={canAdvance}
            canSubmit={canSubmit}
            speechBusy={speechBusy}
            choices={choices}
            onCancel={onCancel}
            onSelect={(value) => {
              setSelectedTransitionKey(value);
              const choice = choices.find((item) => item.transitionKey === value);
              setValues(initialValues(choice?.requiredValues ?? []));
            }}
            onSubmit={submit}
            onValueChange={(nodeKey, outputName, value) => {
              setValues((current) => setValue(current, nodeKey, outputName, value));
            }}
            phase={phase}
            requiredValues={requiredValues}
            selectedChoice={selectedChoice}
            selectedTransitionKey={selectedTransitionKey}
            setPhase={setPhase}
            values={values}
          />
        </MoveDraftContext.Provider>
      )}
    </Dialog>
  );
}

function ManualMoveDialogContent({
  speechBusy,
  canAdvance,
  canSubmit,
  choices,
  onCancel,
  onSelect,
  onSubmit,
  onValueChange,
  phase,
  requiredValues,
  selectedChoice,
  selectedTransitionKey,
  setPhase,
  values,
}: Readonly<{
  speechBusy: boolean;
  canAdvance: boolean;
  canSubmit: boolean;
  choices: readonly TaskMovePreviewChoice[];
  onCancel(): void;
  onSelect(value: string): void;
  onSubmit(): void;
  onValueChange(nodeKey: string, outputName: string, value: string): void;
  phase: "choices" | "details";
  requiredValues: readonly Readonly<{
    nodeKey: string;
    outputName: string;
    description: string | null;
    resolvedValue: string | null;
  }>[];
  selectedChoice: Readonly<{
    label: string;
    sourceNodeDisplayName: string;
  }> | null;
  selectedTransitionKey: string | null;
  setPhase(value: "choices" | "details"): void;
  values: ManualMoveValues;
}>) {
  const { t } = useTranslation();
  const formShortcut = useTextFieldSubmitShortcut({
    available: phase === "details" && canSubmit,
    kind: "form",
  });
  return (
    <form
      className="grid gap-[var(--space-4)]"
      onKeyDown={formShortcut}
      onSubmit={(event) => {
        event.preventDefault();
        if (phase === "details" && canSubmit) {
          onSubmit();
        }
      }}
    >
      <div className="manual-move-dialog-phase" key={phase}>
        {phase === "choices" && choices.length > 1 ? (
          <ManualMoveChoicePhase
            choices={choices}
            onSelect={onSelect}
            selectedTransitionKey={selectedTransitionKey}
          />
        ) : (
          <ManualMoveDetailsPhase
            onValueChange={onValueChange}
            requiredValues={requiredValues}
            selectedChoice={selectedChoice}
            values={values}
          />
        )}
      </div>
      <div className="flex justify-end gap-[var(--space-2)]">
        <Button onClick={onCancel}>{t("app.cancel")}</Button>
        {phase === "choices" ? (
          <Button
            disabled={!canAdvance}
            onClick={() => {
              setPhase("details");
            }}
            variant="primary"
          >
            {t("app.continue")}
          </Button>
        ) : (
          <ControlTooltip label={speechBusy ? t("speech.busy") : t("board.manualMoveConfirm")}>
            <Button disabled={!canSubmit} type="submit" variant="primary">
              {t("board.manualMoveConfirm")}
            </Button>
          </ControlTooltip>
        )}
      </div>
    </form>
  );
}

function manualMoveSubmit(
  transitionKey: string | undefined,
  values: ManualMoveValues,
): ManualMoveDialogSubmit {
  return {
    ...(transitionKey === undefined ? {} : { transitionKey }),
    ...(Object.keys(values).length === 0 ? {} : { values }),
  };
}

function manualMoveChoices(preview: TaskMovePreviewResponse | null): readonly TaskMovePreviewChoice[] {
  if (preview?.outcome === "transition") {
    return preview.transition.choices;
  }
  return [];
}

function manualMoveSelectedChoice(
  choices: readonly TaskMovePreviewChoice[],
  transitionKey: string | null,
): TaskMovePreviewChoice | null {
  return choices.find((choice) => choice.transitionKey === transitionKey) ?? null;
}

function manualMoveRequiredValues(choice: TaskMovePreviewChoice | null) {
  return choice?.requiredValues ?? [];
}

function manualMoveCanAdvance(
  preview: TaskMovePreviewResponse | null,
  choice: TaskMovePreviewChoice | null,
): boolean {
  return preview?.outcome !== "transition" || choice !== null;
}

function manualMoveCanSubmit(
  requiredValues: readonly Readonly<{ nodeKey: string; outputName: string }>[],
  values: ManualMoveValues,
): boolean {
  return requiredValues.every((required) => {
    const value = values[required.nodeKey]?.[required.outputName];
    return value !== undefined && value.trim().length > 0;
  });
}

function initialValues(
  requiredValues: readonly Readonly<{ nodeKey: string; outputName: string; resolvedValue: string | null }>[],
): ManualMoveValues {
  return requiredValues.reduce<Record<string, Record<string, string>>>((result, value) => {
    if (value.resolvedValue !== null) {
      result[value.nodeKey] = { ...(result[value.nodeKey] ?? {}), [value.outputName]: value.resolvedValue };
    }
    return result;
  }, {});
}

function ManualMoveChoicePhase({
  choices,
  onSelect,
  selectedTransitionKey,
}: Readonly<{
  choices: readonly TaskMovePreviewChoice[];
  onSelect(value: string): void;
  selectedTransitionKey: string | null;
}>) {
  const { t } = useTranslation();
  return (
    <RadioGroup
      aria-label={t("board.manualMoveTransitionChoices")}
      onValueChange={onSelect}
      value={selectedTransitionKey}
    >
      {choices.map((choice) => (
        <label className="flex items-start gap-[var(--space-2)]" key={choice.transitionKey}>
          <RadioGroupItem value={choice.transitionKey} />
          <span>
            <strong>{choiceDisplayLabel(choice, choices)}</strong>
          </span>
        </label>
      ))}
    </RadioGroup>
  );
}

function choiceDisplayLabel(
  choice: TaskMovePreviewChoice,
  choices: readonly TaskMovePreviewChoice[],
): string {
  const duplicate = choices.some((candidate) => candidate !== choice && candidate.label === choice.label);
  return duplicate ? `${choice.label} · ${choice.sourceNodeDisplayName}` : choice.label;
}

function ManualMoveDetailsPhase({
  onValueChange,
  requiredValues,
  selectedChoice,
  values,
}: Readonly<{
  onValueChange(nodeKey: string, outputName: string, value: string): void;
  requiredValues: readonly Readonly<{
    nodeKey: string;
    outputName: string;
    description: string | null;
    resolvedValue: string | null;
  }>[];
  selectedChoice: Readonly<{
    label: string;
    sourceNodeDisplayName: string;
  }> | null;
  values: ManualMoveValues;
}>) {
  const { t } = useTranslation();
  return (
    <>
      {selectedChoice === null ? null : (
        <p className="m-0 text-[var(--color-muted)]">
          {selectedChoice.label} · {selectedChoice.sourceNodeDisplayName}
        </p>
      )}
      {requiredValues.length === 0 ? (
        <p className="m-0 text-[var(--color-muted)]">{t("board.manualMoveConfirmBody")}</p>
      ) : (
        <div className="grid gap-[var(--space-4)]">
          {requiredValues.map((value) => (
            <ManualMoveField
              nodeKey={value.nodeKey}
              key={`${value.nodeKey}:${value.outputName}`}
              label={value.outputName}
              hint={value.description ?? undefined}
              onChange={(text) => {
                onValueChange(value.nodeKey, value.outputName, text);
              }}
              value={values[value.nodeKey]?.[value.outputName] ?? value.resolvedValue ?? ""}
            />
          ))}
        </div>
      )}
    </>
  );
}

function ManualMoveField({
  nodeKey,
  label,
  hint,
  value,
  onChange,
}: Readonly<{
  nodeKey: string;
  label: string;
  hint: string | undefined;
  value: string;
  onChange(text: string): void;
}>) {
  const capture = useContext(MoveDraftContext);
  if (capture === null) throw new Error("Manual move draft owner is required.");
  const editorRef = useRef<HTMLTextAreaElement>(null);
  const { control } = useSpeechField({
    identity: JSON.stringify([nodeKey, label]),
    value,
    onChange,
    editorRef,
    captureDraft: () => ({ ...capture(nodeKey, label), editor: () => editorRef.current }),
  });
  return (
    <TextArea
      label={label}
      hint={hint}
      value={value}
      ref={editorRef}
      floatingAction={control}
      onChange={(event) => {
        onChange(event.target.value);
      }}
      required
      rows={2}
    />
  );
}

function setValue(
  values: ManualMoveValues,
  nodeKey: string,
  outputName: string,
  value: string,
): ManualMoveValues {
  return {
    ...values,
    [nodeKey]: { ...(values[nodeKey] ?? {}), [outputName]: value },
  };
}
