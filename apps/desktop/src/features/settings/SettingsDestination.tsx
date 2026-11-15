import { useId, useMemo, useState, type ReactNode, type RefCallback } from "react";
import { useScrollSpy } from "@mantine/hooks";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { ArrowUpRight } from "lucide-react";
import {
  useAppServices,
  useQueryAction,
  useStatusController,
  type DesktopSettings,
  type SettingsChange,
} from "@/app-facade";
import { errorMessage } from "@/api";
import { ConnectionForm } from "@/shared/connection";
import {
  Button,
  DisabledInteractionGuard,
  ErrorState,
  LoadingState,
  PreferenceRow,
  SegmentedControl,
  Spinner,
  Switch,
  cx,
  prefersReducedMotion,
} from "@/ui";
import { createSettingsViewModel, type SettingsViewModel } from "./SettingsViewModel";

const sections = [
  { id: "general", title: "settings.general" },
  { id: "appearance", title: "settings.appearance" },
] as const;

export function SettingsDestination({
  active,
  configPath,
  children,
}: Readonly<{
  active: boolean;
  configPath: string | null;
  children(slots: Readonly<{ page: ReactNode; sidebar: ReactNode }>): ReactNode;
}>): ReactNode {
  const services = useAppServices();
  const client = useQueryClient();
  const status = useStatusController();
  const { t } = useTranslation();
  const model = useMemo(
    () => createSettingsViewModel({ services, client, configPath, status, t }),
    [services, client, configPath, status, t],
  );
  const scrollID = useId();
  const [scrollHost, setScrollHost] = useState<HTMLDivElement | null>(null);
  const spy = useScrollSpy({
    selector: `[data-settings-scroll="${scrollID}"] [data-settings-heading]`,
    ...(scrollHost === null ? {} : { scrollHost }),
  });
  const activeSection = scrollHost === null ? undefined : spy.data[spy.active]?.id;
  return children({
    page: active ? <SettingsPage model={model} scrollRef={setScrollHost} scrollID={scrollID} /> : null,
    sidebar: active ? (
      <nav className="grid gap-[var(--space-1)] py-[var(--space-5)]">
        {sections.map((section) => (
          <button
            className={cx(
              "rounded-[var(--radius-m)] px-[var(--space-3)] py-[var(--space-2)] text-left text-sm text-[var(--color-muted)] transition-colors hover:text-[var(--color-on-island)]",
              activeSection === `${scrollID}-${section.id}` &&
                "bg-[color-mix(in_srgb,var(--color-primary)_12%,transparent)] font-bold text-[var(--color-primary)]",
            )}
            aria-current={activeSection === `${scrollID}-${section.id}` ? "location" : undefined}
            key={section.id}
            onClick={() => {
              const container = scrollHost;
              const heading = spy.data.find((entry) => entry.id === `${scrollID}-${section.id}`)?.getNode();
              if (
                container === null ||
                heading === undefined ||
                container.scrollHeight <= container.clientHeight
              )
                return;
              const top =
                heading.getBoundingClientRect().top -
                container.getBoundingClientRect().top +
                container.scrollTop;
              container.scrollTo({ top, behavior: prefersReducedMotion() ? "instant" : "smooth" });
            }}
          >
            {t(section.title)}
          </button>
        ))}
      </nav>
    ) : null,
  });
}

function SettingsPage({
  model,
  scrollRef,
  scrollID,
}: Readonly<{
  model: SettingsViewModel;
  scrollRef: RefCallback<HTMLDivElement>;
  scrollID: string;
}>) {
  const { t } = useTranslation();
  const settings = useAtomValue(model.settings);
  const config = useAtomValue(model.config);
  const retry = useAtomSet(model.retry);
  const retryConfig = useAtomSet(model.retryConfig);
  const change = useQueryAction(model.change);
  const open = useQueryAction(model.openConfig);
  if (settings.isError || config.isError)
    return (
      <ErrorState
        title={t("settings.title")}
        body={errorMessage(settings.error ?? config.error)}
        onRetry={() => {
          retry();
          retryConfig();
        }}
        retryLabel={t("app.retry")}
      />
    );
  if (settings.data === undefined || config.isPending) return <LoadingState />;
  const saved = settings.data.saved;
  const busy = change.isPending;
  const overridden = saved.lowPower ? t("settings.overridden") : undefined;
  const submit = (input: SettingsChange) => {
    change.submit(input);
  };
  return (
    <div
      ref={scrollRef}
      data-settings-scroll={scrollID}
      className="h-full min-h-0 overflow-auto p-[var(--space-5)]"
    >
      <div className="grid min-w-0 gap-[var(--space-7)]">
        <h1 className="m-0 flex items-center gap-[var(--space-2)] text-2xl font-semibold">
          {t("settings.title")}
          {change.isPending && <Spinner size="sm" />}
        </h1>
        <section className="grid gap-[11px]">
          <h2
            className="m-0 text-xl font-semibold"
            id={`${scrollID}-${sections[0].id}`}
            data-settings-heading
          >
            {t(sections[0].title)}
          </h2>
          <div className="grid">
            <ConfigNotice
              path={config.data.path}
              available={config.data.available}
              opening={open.isPending}
              onOpen={() => {
                open.submit(undefined);
              }}
            />
            <div className="py-[var(--space-2)]">
              <ConnectionForm />
            </div>
            <div className="grid auto-rows-fr py-[var(--space-1)]">
              <PreferenceRow
                label={t("settings.notifications")}
                help={t("settings.help.notifications")}
                disabled={busy}
              >
                <SegmentedControl<DesktopSettings["notifications"]>
                  ariaLabel={t("settings.notifications")}
                  value={saved.notifications}
                  onValueChange={(value) => {
                    submit({ field: "notifications", value });
                  }}
                  options={[
                    { value: "automatic", label: t("settings.automatic") },
                    { value: "native", label: t("settings.native") },
                    { value: "inApp", label: t("settings.inApp") },
                  ]}
                />
              </PreferenceRow>
              <PreferenceRow
                label={t("settings.projectOpening")}
                help={t("settings.help.projectOpening")}
                disabled={busy}
              >
                <SegmentedControl<DesktopSettings["projectOpening"]>
                  ariaLabel={t("settings.projectOpening")}
                  value={saved.projectOpening}
                  onValueChange={(value) => {
                    submit({ field: "projectOpening", value });
                  }}
                  options={[
                    { value: "lastUsed", label: t("settings.lastUsed") },
                    { value: "tasks", label: t("settings.tasks") },
                    { value: "sessions", label: t("settings.sessions") },
                  ]}
                />
              </PreferenceRow>
            </div>
          </div>
        </section>
        <section className="grid gap-[11px]">
          <h2
            className="m-0 text-xl font-semibold"
            id={`${scrollID}-${sections[1].id}`}
            data-settings-heading
          >
            {t(sections[1].title)}
          </h2>
          <div className="grid auto-rows-fr py-[var(--space-1)]">
            <PreferenceRow label={t("settings.theme")} disabled={busy}>
              <SegmentedControl<DesktopSettings["theme"]>
                ariaLabel={t("settings.theme")}
                value={saved.theme}
                onValueChange={(value) => {
                  submit({ field: "theme", value });
                }}
                options={[
                  { value: "system", label: t("settings.system") },
                  { value: "light", label: t("settings.light") },
                  { value: "dark", label: t("settings.dark") },
                ]}
              />
            </PreferenceRow>
            <PreferenceRow label={t("settings.lowPower")} help={t("settings.help.lowPower")} disabled={busy}>
              <Switch
                checked={saved.lowPower}
                aria-label={t("settings.lowPower")}
                onCheckedChange={(value) => {
                  submit({ field: "lowPower", value });
                }}
              />
            </PreferenceRow>
            <PreferenceRow
              label={t("settings.transparency")}
              help={t("settings.help.transparency")}
              disabled={busy}
              disabledReason={overridden}
            >
              <Switch
                checked={saved.transparency}
                aria-label={t("settings.transparency")}
                disabled={saved.lowPower}
                onCheckedChange={(value) => {
                  submit({ field: "transparency", value });
                }}
              />
            </PreferenceRow>
            <PreferenceRow
              label={t("settings.reducedMotion")}
              help={t("settings.help.reducedMotion")}
              disabled={busy}
              disabledReason={overridden}
            >
              <Switch
                checked={saved.reducedMotion}
                aria-label={t("settings.reducedMotion")}
                disabled={saved.lowPower}
                onCheckedChange={(value) => {
                  submit({ field: "reducedMotion", value });
                }}
              />
            </PreferenceRow>
          </div>
        </section>
      </div>
    </div>
  );
}

function ConfigNotice({
  path,
  available,
  opening,
  onOpen,
}: Readonly<{
  path: string | null;
  available: boolean;
  opening: boolean;
  onOpen(): void;
}>) {
  const { t } = useTranslation();
  if (path === null) return null;
  return (
    <div className="flex w-full items-center">
      <DisabledInteractionGuard
        className="w-full"
        disabled={!available || opening}
        reason={!available ? t("settings.configUnavailable") : undefined}
      >
        <Button
          className="w-full px-0! text-sm disabled:opacity-100!"
          variant="ghost"
          disabled={!available || opening}
          onClick={onOpen}
          loading={opening}
        >
          <span className="flex w-full items-center justify-between gap-[var(--space-3)] text-left">
            <span>{t("settings.serverConfig")}</span>
            <span className="inline-flex shrink-0 items-center gap-[var(--space-1)] text-[var(--color-primary)]">
              {t("settings.open")}
              <ArrowUpRight size={16} />
            </span>
          </span>
        </Button>
      </DisabledInteractionGuard>
    </div>
  );
}
