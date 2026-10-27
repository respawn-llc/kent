import { useMemo, type CSSProperties } from "react";
import { useAtomValue, useAtomSet, useAtomMount } from "@effect/atom-react";
import * as Atom from "effect/reactivity/Atom";
import * as AsyncResult from "effect/reactivity/AsyncResult";
import * as Stream from "effect/Stream";
import * as Clock from "effect/Clock";
import { RotateCw } from "lucide-react";
import { AnimatePresence } from "motion/react";
import { useTranslation } from "react-i18next";
import { errorMessage, type SubscriptionUsage, type SubscriptionWindow } from "@/api";
import { formatDuration } from "@/app-facade";
import {
  AnimatedChipSummary,
  AnimatedReveal,
  AnimatedSize,
  Button,
  ErrorState,
  LinearBar,
  Spinner,
  VirtualizedInfiniteList,
} from "@/ui";
import type { createSubscriptionUsageModel } from "./model";
import { presentUsageWindow } from "./presentation";
import "./subscriptionUsage.css";

export type UsageModel = ReturnType<typeof createSubscriptionUsageModel>;

export function UsageContent({
  model,
  virtualized = false,
}: Readonly<{ model: UsageModel; virtualized?: boolean }>) {
  const { t } = useTranslation();
  const state = useAtomValue(model.state);
  const busy = useAtomValue(model.busy);
  const retry = useAtomSet(model.retry);
  useAtomMount(model.open);
  const rows = state.rows.filter((row) => row.data !== null || row.error !== null);
  const viewportStyle: CSSProperties & Record<"--usage-row-count", number> = {
    "--usage-row-count": Math.max(1, Math.min(5, rows.length)),
  };
  if (!virtualized && state.hidden) return null;
  return (
    <AnimatedSize>
      <section className="subscription-usage">
        <header className="subscription-usage-header">
          <strong>{t("subscriptionUsage.title")}</strong>
          {state.failed ? (
            <Button
              onClick={() => {
                retry(undefined);
              }}
              size="icon-sm"
              loading={busy}
              disabled={busy}
              className="transition-transform active:scale-90"
            >
              <RotateCw size={14} />
            </Button>
          ) : busy ? (
            <Spinner size="sm" />
          ) : null}
        </header>
        {state.error !== null ? (
          <ErrorState surface="plain" fullPage={false} body={errorMessage(state.error)} />
        ) : virtualized ? (
          <div className="subscription-usage-viewport" style={viewportStyle}>
            <VirtualizedInfiniteList
              items={rows}
              getItemKey={(row) => row.id}
              renderItem={(row) => <ConnectionUsage row={row} />}
              estimateSize={() => 112}
              hasNextPage={false}
              isFetchingNextPage={false}
              onLoadMore={() => undefined}
              rowSpacing="tight"
              className="h-full min-h-0 overflow-x-hidden overflow-y-auto overscroll-contain"
            />
          </div>
        ) : (
          rows.map((row) => <ConnectionUsage key={row.id} row={row} />)
        )}
      </section>
    </AnimatedSize>
  );
}

type UsageRow = Readonly<{ id: string; data: SubscriptionUsage | null | undefined; error: unknown }>;

function ConnectionUsage({ row }: Readonly<{ row: UsageRow }>) {
  return (
    <AnimatedReveal>
      <AnimatedSize contentClassName="subscription-usage-connection">
        <div className="subscription-usage-name">
          <strong title={row.id}>{row.id}</strong>
          {row.data?.plan != null && (
            <span title={row.data.plan}>
              <AnimatedChipSummary text={row.data.plan} />
            </span>
          )}
        </div>
        {row.data !== undefined && row.data !== null && <UsageWindows usage={row.data} />}
        {row.error !== null && (
          <div className="grid">
            <AnimatePresence>
              <AnimatedReveal
                key={errorMessage(row.error)}
                className="subscription-usage-error col-start-1 row-start-1"
                variant="fade"
              >
                {errorMessage(row.error)}
              </AnimatedReveal>
            </AnimatePresence>
          </div>
        )}
      </AnimatedSize>
    </AnimatedReveal>
  );
}

function UsageWindows({ usage }: Readonly<{ usage: SubscriptionUsage }>) {
  const clock = useMemo(
    () => Atom.make(Stream.tick("1 second").pipe(Stream.mapEffect(() => Clock.currentTimeMillis))),
    [],
  );
  const tick = useAtomValue(clock);
  const now = AsyncResult.isSuccess(tick) ? tick.value : null;
  return usage.windows.map((window) => (
    <UsageWindow
      key={JSON.stringify([window.bucket, window.durationSeconds, window.limitName, window.meteredFeature])}
      window={window}
      source={usage.source}
      now={now}
    />
  ));
}

function UsageWindow({
  window,
  source,
  now,
}: Readonly<{ window: SubscriptionWindow; source: SubscriptionUsage["source"]; now: number | null }>) {
  const { t, i18n } = useTranslation();
  const presentation = presentUsageWindow(window, source);
  return (
    <div className="subscription-usage-window">
      <div className="subscription-usage-window-label">
        <span>
          {window.bucket === "additional"
            ? t("subscriptionUsage.additional", {
                duration: formatDuration(window.durationSeconds, i18n.language),
              })
            : formatDuration(window.durationSeconds, i18n.language)}
          {window.limitName === null ? null : ` · ${window.limitName}`}
          {window.meteredFeature === null ? null : ` · ${window.meteredFeature}`}
        </span>
        <span>
          <AnimatedChipSummary
            text={t(
              presentation.kind === "remaining"
                ? "subscriptionUsage.remaining"
                : "subscriptionUsage.consumed",
              {
                percent: new Intl.NumberFormat(i18n.language, { maximumFractionDigits: 1 }).format(
                  presentation.percent,
                ),
              },
            )}
          />
        </span>
      </div>
      <LinearBar segments={presentation.segments} />
      {((window.resetAt !== null && now !== null) || presentation.contributions !== null) && (
        <div className="subscription-usage-details">
          {presentation.contributions !== null && (
            <span>
              <AnimatedChipSummary text={t("subscriptionUsage.breakdown", presentation.contributions)} />
            </span>
          )}
          {window.resetAt !== null && now !== null && (
            <span>
              <AnimatedChipSummary
                text={t("subscriptionUsage.reset", {
                  duration: formatDuration(Math.max(0, (window.resetAt - now) / 1000), i18n.language),
                })}
              />
            </span>
          )}
        </div>
      )}
    </div>
  );
}
