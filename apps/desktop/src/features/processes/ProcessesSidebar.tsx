import { Circle, Square } from "lucide-react";
import { useMemo, type ReactElement } from "react";
import { useAtomMount, useAtomValue, useAtomSet } from "@effect/atom-react";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { errorMessage, type ChatSessionTarget, type DesktopProcess } from "@/api";
import { useAppServices, useStatusController } from "@/app-facade";
import {
  cx,
  EmptyState,
  ErrorState,
  IconTooltipButton,
  LoadingState,
  Spinner,
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
        body={errorMessage(data.error)}
        fullPage={false}
        onRetry={() => {
          retry(undefined);
        }}
        retryLabel={t("app.retry")}
        title={t("processes.loadFailed")}
      />
    );
  }
  if (data.kind === "loading") {
    return <LoadingState fullPage={false} title={t("processes.loading")} />;
  }

  if (data.processes.length === 0) {
    return <EmptyState body={t("processes.emptyBody")} fullPage={false} title={t("processes.emptyTitle")} />;
  }

  const observationTime = data.observationTime;
  return (
    <VirtualizedInfiniteList
      ariaLabel={t("processes.title")}
      className="h-full min-h-0 overflow-auto px-[var(--space-4)] py-[var(--space-3)] hide-scrollbar contain-strict [-webkit-overflow-scrolling:touch]"
      estimateSize={() => processRowEstimatedHeightPx}
      getItemKey={(process) => process.id}
      hasNextPage={false}
      isFetchingNextPage={false}
      items={data.processes}
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
    <div className="grid h-[76px] min-w-0 grid-rows-[28px_22px_22px] border-b border-[var(--color-outline)] text-sm">
      <div className="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-center gap-[var(--space-2)]">
        <div className="flex min-w-0 items-center gap-[var(--space-2)]">
          <ProcessStateIcon indicator={presentation.stateIndicator} tone={presentation.stateTone} />
          <span className={cx("shrink-0", processStateToneClassName[presentation.stateTone])}>
            {presentation.stateLabel}
          </span>
          <span className="min-w-0 truncate font-mono font-semibold">{presentation.id}</span>
          {presentation.age === null ? null : (
            <span className="shrink-0 text-[var(--color-muted)]">{presentation.age}</span>
          )}
          {presentation.workdir.length === 0 ? null : (
            <span className="min-w-0 truncate text-[var(--color-muted)]">{presentation.workdir}</span>
          )}
        </div>
        {pending ? (
          <Spinner size="sm" />
        ) : presentation.stopping ? (
          <span className="shrink-0 text-xs text-[var(--color-muted)]">{t("processes.stopping")}</span>
        ) : presentation.terminable ? (
          <IconTooltipButton
            label={t("processes.terminate", { id: processID })}
            onClick={() => {
              terminate(undefined);
            }}
            size="icon-sm"
            variant="danger"
          >
            <Square aria-hidden="true" fill="currentColor" size={11} strokeWidth={0} />
          </IconTooltipButton>
        ) : null}
      </div>
      <div className="min-w-0 truncate font-mono text-[var(--color-on-island)]">
        <span className="mr-[var(--space-2)] text-[var(--color-primary)]">$</span>
        {presentation.command}
      </div>
      <div className="min-w-0 truncate font-mono text-[var(--color-muted)]">{presentation.output}</div>
    </div>
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
