import { Plus, X } from "lucide-react";
import { useId } from "react";
import { useTranslation } from "react-i18next";

import type {
  TaskDependencies,
  TaskDependencyDirection,
  TaskDependencyDirectionProjection,
  TaskDependencyItem,
} from "@/api";
import { TaskStatusIcon } from "@/shared/task-status";
import { ActionableListRow, Button, Island, Spinner } from "@/ui";
import { TaskDependencyPicker } from "./TaskDependencyPicker";
import { TaskDependencyProgressChip } from "./TaskDependencyProgressChip";
import { requiredTaskDependencyDirection, taskDependencyPairForDirection } from "./dependencyCache";
import { useTaskDependencyActions, type DependencyInteraction } from "./dependencyActions";

export function DependenciesArea({
  dependencies,
  excludedTaskIDs,
  navigationDisabled = false,
  onAdd,
  interaction,
  onSelectTask,
  previewProgress = false,
  projectID,
}: Readonly<{
  dependencies: TaskDependencies;
  excludedTaskIDs(direction: TaskDependencyDirection): ReadonlySet<string>;
  navigationDisabled?: boolean;
  onAdd(direction: TaskDependencyDirection): void;
  interaction: DependencyInteraction;
  onSelectTask(taskID: string): void;
  previewProgress?: boolean | undefined;
  projectID: string;
}>) {
  const { t } = useTranslation();
  const blockedBy = requiredTaskDependencyDirection(dependencies, "blocked-by");
  const blocks = requiredTaskDependencyDirection(dependencies, "blocks");
  return (
    <Island className="grid gap-[var(--space-3)] p-[var(--space-3)]" level={1} radius="l">
      <header className="flex min-w-0 items-center justify-between gap-[var(--space-2)]">
        <h2 className="m-0 text-base font-semibold">{t("task.dependencies")}</h2>
        {dependencies.blockerCount === 0 ? null : (
          <TaskDependencyProgressChip
            preview={previewProgress}
            progress={{
              satisfiedCount: dependencies.blockerCount - dependencies.unsatisfiedBlockerCount,
              totalCount: dependencies.blockerCount,
            }}
          />
        )}
      </header>
      <DependencyDirection
        direction={blockedBy}
        excludedTaskIDs={excludedTaskIDs("blocked-by")}
        navigationDisabled={navigationDisabled}
        onAdd={onAdd}
        interaction={interaction}
        onSelectTask={onSelectTask}
        projectID={projectID}
      />
      <div className="h-px bg-[var(--color-outline)]" />
      <DependencyDirection
        direction={blocks}
        excludedTaskIDs={excludedTaskIDs("blocks")}
        navigationDisabled={navigationDisabled}
        onAdd={onAdd}
        interaction={interaction}
        onSelectTask={onSelectTask}
        projectID={projectID}
      />
    </Island>
  );
}

function DependencyDirection({
  direction,
  excludedTaskIDs,
  navigationDisabled,
  onAdd,
  interaction,
  onSelectTask,
  projectID,
}: Readonly<{
  direction: TaskDependencyDirectionProjection;
  excludedTaskIDs: ReadonlySet<string>;
  navigationDisabled: boolean;
  onAdd(direction: TaskDependencyDirection): void;
  interaction: DependencyInteraction;
  onSelectTask(taskID: string): void;
  projectID: string;
}>) {
  const { t } = useTranslation();
  const headingID = useId();
  const unavailableID = useId();
  const limitReached = direction.addAvailability.kind === "limit_reached";
  const trigger = (
    <Button
      aria-describedby={limitReached ? unavailableID : undefined}
      aria-label={t("task.dependenciesAdd")}
      data-testid={`dependency-add-${direction.direction}`}
      disabled={navigationDisabled || limitReached}
      size="icon-sm"
      variant="ghost"
    >
      <Plus aria-hidden="true" size={15} />
    </Button>
  );
  return (
    <section aria-labelledby={headingID} data-direction={direction.direction} role="group">
      <header className="mb-[var(--space-1)] flex items-center justify-between gap-[var(--space-2)]">
        <h3 className="m-0 text-sm font-semibold" id={headingID}>
          {t(
            direction.direction === "blocked-by" ? "task.dependenciesBlockedBy" : "task.dependenciesBlocks",
            { count: direction.totalCount },
          )}
        </h3>
        {limitReached ? (
          trigger
        ) : (
          <TaskDependencyPicker
            disabled={navigationDisabled}
            excludedTaskIDs={excludedTaskIDs}
            onCreateTask={() => {
              onAdd(direction.direction);
            }}
            interaction={interaction}
            direction={direction.direction}
            projectID={projectID}
            trigger={trigger}
          />
        )}
        {limitReached ? (
          <span className="sr-only" id={unavailableID}>
            {t("task.dependenciesLimitReached")}
          </span>
        ) : null}
      </header>
      <div className="grid gap-[2px]">
        {direction.items.map((item) => (
          <DependencyActionRow
            direction={direction.direction}
            item={item}
            key={item.taskID}
            navigationDisabled={navigationDisabled}
            interaction={interaction}
            projectID={projectID}
            onSelectTask={onSelectTask}
          />
        ))}
      </div>
    </section>
  );
}

function DependencyRow({
  item,
  navigationDisabled,
  onRemove,
  onSelectTask,
  pending,
}: Readonly<{
  item: TaskDependencyItem;
  navigationDisabled: boolean;
  onRemove(): void;
  pending: boolean;
  onSelectTask(taskID: string): void;
}>) {
  const { t } = useTranslation();
  return (
    <ActionableListRow
      actions={
        <button
          aria-label={t("task.dependenciesRemove")}
          className="grid size-7 place-items-center rounded-[var(--radius-s)] border-0 bg-transparent text-[var(--color-error)] outline-none focus-visible:ring-[3px] focus-visible:ring-[color-mix(in_srgb,var(--color-error)_35%,transparent)] disabled:cursor-not-allowed disabled:opacity-45"
          data-testid={`dependency-remove-${item.taskID}`}
          aria-busy={pending}
          onClick={() => {
            onRemove();
          }}
          type="button"
        >
          {pending ? <Spinner size="sm" /> : <X aria-hidden="true" size={15} />}
        </button>
      }
      data-satisfaction={item.satisfaction ?? undefined}
      selectButtonProps={{
        disabled: navigationDisabled,
        onClick: () => {
          onSelectTask(item.taskID);
        },
      }}
    >
      <span
        className="flex min-w-0 items-center gap-[var(--space-2)]"
        data-testid={`dependency-row-${item.taskID}`}
      >
        <TaskStatusIcon status={item.status.kind} />
        <span className="shrink-0 font-mono text-xs text-[var(--color-muted)]">{item.shortID}</span>
        <span className="min-w-0 truncate">{item.title}</span>
      </span>
    </ActionableListRow>
  );
}

type ActionRowProps = Readonly<{
  direction: TaskDependencyDirection;
  item: TaskDependencyItem;
  navigationDisabled: boolean;
  onSelectTask(taskID: string): void;
  interaction: DependencyInteraction;
  projectID: string;
}>;

function DependencyActionRow(props: ActionRowProps) {
  const { interaction, direction, item } = props;
  return interaction.kind === "persisted" ? (
    <PersistedDependencyRow {...props} interaction={interaction} />
  ) : (
    <DependencyRow
      {...props}
      pending={false}
      onRemove={() => {
        interaction.onRemove(direction, item);
      }}
    />
  );
}

function PersistedDependencyRow({
  interaction,
  ...props
}: ActionRowProps &
  Readonly<{
    interaction: Extract<DependencyInteraction, { kind: "persisted" }>;
  }>) {
  const action = useTaskDependencyActions(
    props.projectID,
    interaction.taskID,
    taskDependencyPairForDirection(interaction.taskID, props.direction, props.item.taskID),
  );
  return (
    <DependencyRow
      {...props}
      pending={action.pending}
      onRemove={() => {
        action.submit({ kind: "remove", onChanged: interaction.onChanged });
      }}
    />
  );
}
