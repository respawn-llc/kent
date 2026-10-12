import { Circle, CornerDownRight, Square } from "lucide-react";
import { useMemo, type ReactElement } from "react";
import { useAtomMount, useAtomValue, useAtomSet } from "@effect/atom-react";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { errorMessage, type ChatSessionTarget, type DesktopProcess } from "@/api";
import { useAppServices, useStatusController } from "@/app-facade";
import {
  ActionableListRow,
  EmptyState,
  ErrorState,
  IconTooltipButton,
  LoadingState,
  Spinner,
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
  VirtualizedInfiniteList,
} from "@/ui";
import {
  projectProcessPresentation,
  type ProcessPresentation,
  type ProcessStateTone,
} from "./processPresentation";
import { createProcessesViewModel, createProcessTermination } from "./ProcessesViewModel";

const processRowEstimatedHeightPx = 80;
const noLoad = () => undefined;

export function ProcessesSidebar({ target }: Readonly<{ target: ChatSessionTarget }>): ReactElement {
  const { t } = useTranslation();
  const { api } = useAppServices();
  const client = useQueryClient();
  const { projectID, sessionID } = target;
  const model = useMemo(
    () =>
      createProcessesViewModel({
        api,
        client,
        target: { projectID, sessionID },
      }),
    [api, client, projectID, sessionID],
  );
  const data = useAtomValue(model.state);
  const retry = useAtomSet(model.retry);

  if (data.kind === "error") {
    return (
      <ErrorState
        inset
        surface="plain"
        body={errorMessage(data.error)}
        onRetry={() => {
          retry(undefined);
        }}
        retryLabel={t("app.retry")}
        title={t("processes.loadFailed")}
      />
    );
  }
  if (data.kind === "loading") {
    return <LoadingState surface="plain" inset title={t("processes.loading")} />;
  }

  if (data.processes.length === 0) {
    return <EmptyState body={t("processes.emptyBody")} fullPage={false} title={t("processes.emptyTitle")} />;
  }

  const observationTime = data.observationTime;
  return (
    <VirtualizedInfiniteList
      ariaLabel={t("processes.title")}
      className="h-full min-h-0 overflow-auto px-[var(--space-4)] py-[var(--space-3)] hide-scrollbar contain-strict [overflow-anchor:none] [-webkit-overflow-scrolling:touch]"
      estimateSize={() => processRowEstimatedHeightPx}
      getItemKey={(process) => process.id}
      hasNextPage={false}
      isFetchingNextPage={false}
      items={data.processes}
      layoutChangeScrollBehavior="natural"
      loadingLabel={t("app.loadingMore")}
      onLoadMore={noLoad}
      rowSpacing="tight"
      renderItem={(process) => <ProcessRow process={process} observationTime={observationTime} />}
    />
  );
}

function ProcessRow({
  process,
  observationTime,
}: Readonly<{
  process: DesktopProcess;
  observationTime: number;
}>): ReactElement {
  const { t } = useTranslation();
  const { push } = useStatusController();
  const { api } = useAppServices();
  const client = useQueryClient();
  const processID = process.id;
  const model = useMemo(
    () =>
      createProcessTermination({
        api,
        client,
        processID,
        onError: (error) => {
          push({
            body: errorMessage(error),
            id: `process-terminate-error-${processID}`,
            title: t("processes.terminateFailed"),
            tone: "danger",
          });
        },
      }),
    [api, client, processID, push, t],
  );
  useAtomMount(model.request);
  const pending = useAtomValue(model.pending);
  const terminate = useAtomSet(model.terminate);
  const presentation = projectProcessPresentation(process, observationTime);
  return (
    <ActionableListRow
      layout="stacked"
      selectionControl={
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <div className="flex min-w-0 items-center gap-[var(--space-2)]">
                <ProcessStateIcon indicator={presentation.stateIndicator} tone={presentation.stateTone} />
                <span className="min-w-0 truncate font-mono text-sm font-medium">{presentation.id}</span>
              </div>
            </TooltipTrigger>
            <TooltipContent className="max-w-sm break-all font-mono">{presentation.workdir}</TooltipContent>
          </Tooltip>
        </TooltipProvider>
      }
      actions={
        <>
          {pending || presentation.stopping || presentation.terminable ? (
            <IconTooltipButton
              className="size-5 text-[var(--color-error)]"
              disabled={pending || presentation.stopping}
              label={
                pending || presentation.stopping
                  ? t("processes.stopping")
                  : t("processes.terminate", { id: processID })
              }
              onClick={() => {
                terminate(undefined);
              }}
              loading={pending || presentation.stopping}
              size="icon-sm"
              variant="ghost"
            >
              <Square size={12} strokeWidth={1.5} />
            </IconTooltipButton>
          ) : null}
          {presentation.age === null ? null : (
            <span className="shrink-0 text-xs tabular-nums text-[var(--color-muted)]">
              {presentation.age}
            </span>
          )}
        </>
      }
      subtitle={
        <div className="grid min-w-0 gap-[var(--space-1)] font-mono text-xs text-[var(--color-muted)]">
          <div className="truncate">{presentation.command}</div>
          {presentation.output.length === 0 ? null : (
            <div className="flex min-w-0 items-center gap-[var(--space-1)]">
              <CornerDownRight className="shrink-0" size={12} strokeWidth={1.5} />
              <span className="min-w-0 truncate">{presentation.output}</span>
            </div>
          )}
        </div>
      }
    />
  );
}

function ProcessStateIcon({
  indicator,
  tone,
}: Readonly<{
  indicator: ProcessPresentation["stateIndicator"];
  tone: ProcessStateTone;
}>): ReactElement {
  const className = processStateToneClassName[tone];
  if (indicator === "active") {
    return <Spinner className={className} size="sm" strokeWidth={2.5} testID="process-state-spinner" />;
  }
  return <Circle aria-hidden="true" className={className} fill="currentColor" size={10} strokeWidth={0} />;
}

const processStateToneClassName = {
  error: "text-[var(--color-error)]",
  muted: "text-[var(--color-muted)]",
  primary: "text-[var(--color-primary)]",
  success: "text-[var(--color-success)]",
} satisfies Record<ProcessStateTone, string>;
