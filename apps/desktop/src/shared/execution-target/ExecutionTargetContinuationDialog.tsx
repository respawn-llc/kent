import { useState } from "react";
import { useTranslation } from "react-i18next";

import type {
  WorkflowExecutionTargetSelection,
  WorkflowExecutionTargetSelectionMode,
  WorkflowExecutionTargetSelectionRequirement,
  WorktreeSetupRetainedError,
  ExecutionTargetChoiceFailure,
} from "@/api";
import { useTextFieldSubmitShortcut } from "@/app-facade";
import { Button, compactDialogWidth, Dialog, RadioGroup, RadioGroupItem, Spinner, TextInput } from "@/ui";
import {
  executionTargetSelectionFromDraft,
  executionTargetBranchName,
  taskActionWithBranchName,
  proceedWithTaskInitiatingAction,
  type ExecutionTargetSelectionDraft,
  type TaskInitiatingAction,
} from "./executionTargetContinuation";
import type {
  PendingTaskInitiatingAction,
  TaskInitiatingActionController,
} from "./useExecutionTargetContinuation";

const concreteModes = ["none", "head", "default_branch", "custom_ref"] as const;
type ExecutionTargetPending = Extract<PendingTaskInitiatingAction, { kind: "execution_target" }>;

export function TaskSetupRecoveryDialog({
  onClose,
  onSubmit,
  open,
  recovery,
  retrySelection,
  running,
  choiceFailure,
}: Readonly<{
  onClose(): void;
  onSubmit(selection?: WorkflowExecutionTargetSelection, branchName?: string): void;
  choiceFailure: ExecutionTargetChoiceFailure | null;
  open: boolean;
  recovery: WorktreeSetupRetainedError;
  retrySelection?: WorkflowExecutionTargetSelection;
  running: boolean;
}>) {
  const { t } = useTranslation();
  const [selectionDraft, setSelectionDraft] = useState<ExecutionTargetSelectionDraft | null>(null);
  const [branchName, setBranchName] = useState<string | null>(null);
  const close = () => {
    setSelectionDraft(null);
    setBranchName(null);
    onClose();
  };
  const selection = selectionDraft === null ? null : executionTargetSelectionFromDraft(selectionDraft);
  return (
    <Dialog
      closeLabel={t("app.close")}
      onClose={close}
      open={open}
      title={t("executionTargetContinuation.preparationFailed")}
    >
      <div className="grid gap-[var(--space-3)]">
        <p className="m-0 whitespace-pre-wrap font-mono text-sm text-[var(--color-error)]">
          {recovery.diagnostic}
        </p>
        {[
          recovery.scriptPath,
          recovery.worktree.kent.canonicalRoot,
          recovery.retainedPreviousWorktree?.worktree.kent.canonicalRoot,
        ].map((value) =>
          value == null ? null : (
            <p className="m-0 break-all font-mono text-sm" key={value}>
              {value}
            </p>
          ),
        )}
        {selectionDraft === null ? (
          <div className="flex justify-end gap-[var(--space-2)]">
            <Button onClick={close}>{t("app.cancel")}</Button>
            <Button
              data-testid="setup-recovery-choose"
              onClick={() => {
                setSelectionDraft({ mode: "default_branch", customRef: null });
              }}
            >
              {t("executionTargetContinuation.title")}
            </Button>
            {recovery.recoveryDisposition === "retry_existing" ? (
              <Button
                data-testid="setup-recovery-retry"
                aria-busy={running}
                onClick={() => {
                  onSubmit(retrySelection);
                }}
                variant="primary"
              >
                {running ? <Spinner /> : t("app.retry")}
              </Button>
            ) : null}
          </div>
        ) : (
          <>
            <ExecutionTargetChoices
              continuation={{
                selectMode(mode) {
                  setSelectionDraft({ ...selectionDraft, mode });
                },
                setCustomRef(customRef) {
                  setSelectionDraft({ ...selectionDraft, customRef });
                },
              }}
              pending={{ selection: selectionDraft }}
              branch={
                recovery.recoveryDisposition === "fresh_replacement"
                  ? { value: branchName, onChange: setBranchName }
                  : undefined
              }
            />
            <ExecutionTargetChoiceFailureMessage failure={choiceFailure} />
            <div className="flex justify-end gap-[var(--space-2)]">
              <Button onClick={close}>{t("app.cancel")}</Button>
              <Button
                data-testid="setup-recovery-target-submit"
                disabled={selection === null}
                aria-busy={running}
                onClick={() => {
                  if (selection !== null)
                    onSubmit(selection, executionTargetBranchName(selection, branchName));
                }}
                variant="primary"
              >
                {running ? <Spinner /> : t("executionTargetContinuation.continue")}
              </Button>
            </div>
          </>
        )}
      </div>
    </Dialog>
  );
}

export type TaskInitiatingActionDialogResult =
  | Readonly<{
      kind: "continue";
      action: TaskInitiatingAction;
      selection?: WorkflowExecutionTargetSelection;
    }>
  | Readonly<{
      kind: "view_dependencies";
      taskID: string;
    }>;

export function TaskInitiatingActionDialogs({
  continuation,
  onResult,
}: Readonly<{
  continuation: TaskInitiatingActionController;
  onResult(result: TaskInitiatingActionDialogResult): void;
}>) {
  const pending = continuation.pending;
  if (pending?.kind === "dependency_confirmation") {
    return <DependencyConfirmationDialog continuation={continuation} onResult={onResult} pending={pending} />;
  }
  if (pending?.kind === "setup_recovery") {
    const { failure } = pending;
    return (
      <TaskSetupRecoveryDialog
        onClose={continuation.close}
        choiceFailure={pending.choiceFailure}
        onSubmit={(selection, branchName) => {
          onResult({
            kind: "continue",
            action:
              failure.recoveryDisposition === "fresh_replacement" && selection !== undefined
                ? taskActionWithBranchName(pending.action, selection, branchName ?? null)
                : pending.action,
            ...(selection === undefined ? {} : { selection }),
          });
        }}
        open
        recovery={failure}
        {...(pending.retrySelection === undefined ? {} : { retrySelection: pending.retrySelection })}
        running={continuation.running}
      />
    );
  }
  return (
    <ExecutionTargetDialog
      continuation={continuation}
      onResult={onResult}
      pending={pending?.kind === "execution_target" ? pending : null}
    />
  );
}

function DependencyConfirmationDialog({
  continuation,
  onResult,
  pending,
}: Readonly<{
  continuation: TaskInitiatingActionController;
  onResult(result: TaskInitiatingActionDialogResult): void;
  pending: Extract<PendingTaskInitiatingAction, { kind: "dependency_confirmation" }>;
}>) {
  const { t } = useTranslation();
  const taskID = pending.action.kind === "move" ? pending.action.input.taskID : pending.action.taskID;
  return (
    <Dialog
      closeLabel={t("app.close")}
      onClose={continuation.close}
      open
      title={t("taskDependencyConfirmation.title")}
      width={compactDialogWidth}
    >
      <div className="grid gap-[var(--space-4)]">
        <p className="m-0 text-[var(--color-muted)]">
          {t("taskDependencyConfirmation.body", {
            count: pending.unsatisfiedDependencyCount,
          })}
        </p>
        <div className="flex justify-end gap-[var(--space-2)]">
          <Button
            data-testid="dependency-confirmation-view"
            onClick={() => {
              continuation.close();
              onResult({ kind: "view_dependencies", taskID });
            }}
            variant="primary-outline"
          >
            {t("taskDependencyConfirmation.view")}
          </Button>
          <Button
            data-testid="dependency-confirmation-proceed"
            onClick={() => {
              continuation.close();
              onResult({
                kind: "continue",
                action: proceedWithTaskInitiatingAction(pending.action),
              });
            }}
            variant="primary"
          >
            {t("taskDependencyConfirmation.start")}
          </Button>
        </div>
      </div>
    </Dialog>
  );
}

function ExecutionTargetDialog({
  continuation,
  onResult,
  pending,
}: Readonly<{
  continuation: TaskInitiatingActionController;
  onResult(result: TaskInitiatingActionDialogResult): void;
  pending: ExecutionTargetPending | null;
}>) {
  const { t } = useTranslation();
  return (
    <Dialog
      closeLabel={t("app.close")}
      onClose={continuation.close}
      open={pending !== null}
      title={t("executionTargetContinuation.title")}
    >
      {pending === null ? null : (
        <ExecutionTargetForm continuation={continuation} onResult={onResult} pending={pending} />
      )}
    </Dialog>
  );
}

function ExecutionTargetForm({
  continuation,
  onResult,
  pending,
}: Readonly<{
  continuation: TaskInitiatingActionController;
  onResult(result: TaskInitiatingActionDialogResult): void;
  pending: ExecutionTargetPending;
}>) {
  const { t } = useTranslation();
  const [branchName, setBranchName] = useState<string | null>(
    pending.action.kind === "move"
      ? (pending.action.input.branchName ?? null)
      : pending.action.kind === "resume"
        ? (pending.action.branchName ?? null)
        : null,
  );
  const replacement = pending.requirement.reason === "original_target_unavailable";
  const selectedTarget = executionTargetSelectionFromDraft(pending.selection);
  const canSubmit = selectedTarget !== null;
  const formShortcut = useTextFieldSubmitShortcut({
    available: canSubmit,
    kind: "form",
  });
  return (
    <form
      className="grid gap-[var(--space-4)]"
      onKeyDown={formShortcut}
      onSubmit={(event) => {
        event.preventDefault();
        if (!canSubmit) {
          return;
        }
        onResult({
          kind: "continue",
          action: replacement
            ? taskActionWithBranchName(pending.action, selectedTarget, branchName)
            : pending.action,
          selection: selectedTarget,
        });
      }}
    >
      <ExecutionTargetRequirementMessage requirement={pending.requirement} />
      <ExecutionTargetChoices
        continuation={continuation}
        pending={pending}
        branch={
          replacement
            ? {
                value: branchName,
                onChange: (value) => {
                  setBranchName(value);
                  continuation.clearChoiceFailure();
                },
              }
            : undefined
        }
      />
      <ExecutionTargetChoiceFailureMessage failure={pending.choiceFailure} />
      <div className="flex justify-end gap-[var(--space-2)]">
        <Button onClick={continuation.close}>{t("app.cancel")}</Button>
        <Button
          data-testid="execution-target-submit"
          disabled={!canSubmit}
          aria-busy={continuation.running}
          type="submit"
          variant="primary"
        >
          {continuation.running ? <Spinner /> : t("executionTargetContinuation.continue")}
        </Button>
      </div>
    </form>
  );
}

function ExecutionTargetChoices({
  continuation,
  pending,
  branch,
}: Readonly<{
  continuation: Pick<TaskInitiatingActionController, "selectMode" | "setCustomRef">;
  pending: Pick<ExecutionTargetPending, "selection">;
  branch?: Readonly<{ value: string | null; onChange(value: string | null): void }> | undefined;
}>) {
  const { t } = useTranslation();
  return (
    <>
      <RadioGroup
        aria-label={t("executionTargetContinuation.choice")}
        onValueChange={(mode) => {
          if (isConcreteMode(mode)) {
            continuation.selectMode(mode);
          }
        }}
        value={pending.selection.mode}
      >
        {concreteModes.map((mode) => (
          <label
            className="grid cursor-pointer grid-cols-[auto_minmax(0,1fr)] items-start gap-x-[var(--space-2)] rounded-[var(--radius-m)] border border-[var(--color-outline)] p-[var(--space-2)] transition-[border-color,background-color] has-[[data-state=checked]]:border-[var(--color-primary)] has-[[data-state=checked]]:bg-[var(--color-island-2)]"
            key={mode}
          >
            <RadioGroupItem className="mt-[2px]" value={mode} />
            <span className="grid gap-[2px]">
              <strong className="text-sm">{t(`executionTargetContinuation.mode_${mode}`)}</strong>
              <span className="text-sm leading-snug text-[var(--color-muted)]">
                {t(`executionTargetContinuation.mode_${mode}Help`)}
              </span>
            </span>
          </label>
        ))}
      </RadioGroup>
      {pending.selection.mode === "custom_ref" ? (
        <TextInput
          label={t("executionTargetContinuation.customRef")}
          onChange={(event) => {
            continuation.setCustomRef(
              event.currentTarget.value.trim().length === 0 ? null : event.currentTarget.value,
            );
          }}
          required
          value={pending.selection.customRef ?? ""}
        />
      ) : null}
      {branch !== undefined && pending.selection.mode !== "none" ? (
        <TextInput
          data-testid="execution-target-branch-name"
          label={t("executionTargetContinuation.branchName")}
          placeholder={t("executionTargetContinuation.branchNameDefault")}
          value={branch.value ?? ""}
          onChange={(event) => {
            branch.onChange(event.currentTarget.value.length === 0 ? null : event.currentTarget.value);
          }}
        />
      ) : null}
    </>
  );
}

function ExecutionTargetChoiceFailureMessage({
  failure,
}: Readonly<{ failure: ExecutionTargetChoiceFailure | null }>) {
  const { t } = useTranslation();
  if (failure === null) return null;
  return (
    <p data-testid="execution-target-choice-error" className="m-0 text-sm text-[var(--color-error)]">
      {failure.kind === "branch"
        ? t(`executionTargetContinuation.branch_${failure.reason}`, { value: failure.value })
        : t(`executionTargetContinuation.revision_${failure.reason}`, { value: failure.value })}
    </p>
  );
}

function ExecutionTargetRequirementMessage({
  requirement,
}: Readonly<{ requirement: WorkflowExecutionTargetSelectionRequirement }>) {
  const { t } = useTranslation();
  if (requirement.reason === "policy_requires_selection") {
    return (
      <p className="m-0 text-[var(--color-muted)]">
        {t("executionTargetContinuation.policyRequiresSelection")}
      </p>
    );
  }
  if (requirement.reason === "original_target_unavailable") {
    return (
      <p className="m-0 text-[var(--color-muted)]">
        {t(`executionTargetContinuation.original_${requirement.originalTargetCause}`)}
      </p>
    );
  }
  return (
    <div className="grid gap-[var(--space-1)]">
      <p className="m-0 text-[var(--color-muted)]">
        {t("executionTargetContinuation.configuredTargetUnavailable")}
      </p>
      <p className="m-0 text-sm text-[var(--color-warning)]">
        {t(`executionTargetContinuation.unavailable_${requirement.unavailableCause}`)}
      </p>
    </div>
  );
}

function isConcreteMode(value: string): value is WorkflowExecutionTargetSelectionMode {
  return concreteModes.some((mode) => mode === value);
}
