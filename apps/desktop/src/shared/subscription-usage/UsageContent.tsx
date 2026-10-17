import { useMemo, type CSSProperties } from "react";
import { useAtomValue, useAtomSet, useAtomMount } from "@effect/atom-react";
import * as Atom from "effect/reactivity/Atom";
import * as AsyncResult from "effect/reactivity/AsyncResult";
import * as Stream from "effect/Stream";
import * as Clock from "effect/Clock";
import { RotateCw } from "lucide-react";
import { useTranslation } from "react-i18next";
import { errorMessage, SubscriptionReadError, type SubscriptionUsage, type SubscriptionWindow } from "@/api";
import { ErrorState, IconTooltipButton, LinearBar, Spinner, VirtualizedInfiniteList } from "@/ui";
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
    <section className="subscription-usage">
      <header className="subscription-usage-header">
        <strong>{t("subscriptionUsage.title")}</strong>
        {state.failed ? (
          <IconTooltipButton
            label={t("subscriptionUsage.retry")}
            onClick={() => {
              retry(undefined);
            }}
            size="icon-sm"
            loading={busy}
            disabled={busy}
          >
            <RotateCw size={14} />
          </IconTooltipButton>
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
            loadingLabel={t("subscriptionUsage.loading")}
            onLoadMore={() => undefined}
            rowSpacing="tight"
            className="h-full"
          />
        </div>
      ) : (
        rows.map((row) => <ConnectionUsage key={row.id} row={row} />)
      )}
    </section>
  );
}

type UsageRow = Readonly<{ id: string; data: SubscriptionUsage | null | undefined; error: unknown }>;

function ConnectionUsage({ row }: Readonly<{ row: UsageRow }>) {
  const { t } = useTranslation();
  return (
    <div className="subscription-usage-connection">
      <div className="subscription-usage-name">
        <strong title={row.id}>{row.id}</strong>
        {row.data?.plan != null && <span title={row.data.plan}>{row.data.plan}</span>}
      </div>
      {row.data !== undefined && row.data !== null && <UsageWindows usage={row.data} />}
      {row.error !== null && (
        <p className="subscription-usage-error">
          {row.error instanceof SubscriptionReadError &&
            row.error.classification === "unavailable-for-sign-in-method" && (
              <span className="block">{t("subscriptionUsage.quotaUnavailableForSignInMethod")}</span>
            )}
          <span>{errorMessage(row.error)}</span>
        </p>
      )}
    </div>
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
            ? t("subscriptionUsage.additional", { duration: duration(window.durationSeconds, i18n.language) })
            : duration(window.durationSeconds, i18n.language)}
          {window.limitName === null ? null : ` · ${window.limitName}`}
          {window.meteredFeature === null ? null : ` · ${window.meteredFeature}`}
        </span>
        <span>
          {t(
            presentation.kind === "remaining" ? "subscriptionUsage.remaining" : "subscriptionUsage.consumed",
            {
              percent: new Intl.NumberFormat(i18n.language, { maximumFractionDigits: 1 }).format(
                presentation.percent,
              ),
            },
          )}
        </span>
      </div>
      <LinearBar segments={presentation.segments} />
      {((window.resetAt !== null && now !== null) || presentation.contributions !== null) && (
        <div className="subscription-usage-details">
          {presentation.contributions !== null && (
            <span>{t("subscriptionUsage.breakdown", presentation.contributions)}</span>
          )}
          {window.resetAt !== null && now !== null && (
            <span>
              {t("subscriptionUsage.reset", {
                duration: duration(Math.max(0, (window.resetAt - now) / 1000), i18n.language),
              })}
            </span>
          )}
        </div>
      )}
    </div>
  );
}

function duration(seconds: number, locale: string) {
  const unit = seconds >= 86400 ? "day" : seconds >= 3600 ? "hour" : seconds >= 60 ? "minute" : "second";
  const divisor = unit === "day" ? 86400 : unit === "hour" ? 3600 : unit === "minute" ? 60 : 1;
  return new Intl.NumberFormat(locale, {
    style: "unit",
    unit,
    unitDisplay: "short",
    maximumFractionDigits: 0,
  }).format(Math.ceil(seconds / divisor));
}
