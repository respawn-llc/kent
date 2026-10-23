import { useMemo, useRef, type ReactNode, type RefObject } from "react";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Info } from "lucide-react";
import {
  useAppServices,
  usePathFormatter,
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
  prefersReducedMotion,
} from "@/ui";
import { createSettingsViewModel, type SettingsViewModel } from "./SettingsViewModel";

const sections = [{ id: "appearance", title: "settings.appearance" }] as const;

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
  const scroll = useRef<HTMLDivElement>(null);
  const appearance = useRef<HTMLElement>(null);
  return children({
    page: active ? <SettingsPage model={model} scroll={scroll} appearance={appearance} /> : null,
    sidebar: active ? (
      <nav className="grid gap-[var(--space-2)] p-[var(--space-3)]">
        {sections.map((section) => (
          <button
            className="text-left"
            key={section.id}
            onClick={() => {
              const container = scroll.current;
              if (
                container === null ||
                appearance.current === null ||
                container.scrollHeight <= container.clientHeight
              )
                return;
              const top =
                appearance.current.getBoundingClientRect().top -
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
  scroll,
  appearance,
}: Readonly<{
  model: SettingsViewModel;
  scroll: RefObject<HTMLDivElement | null>;
  appearance: RefObject<HTMLElement | null>;
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
  if (settings.data === undefined || config.isPending)
    return <LoadingState title={t("settings.title")} body={t("settings.loading")} />;
  const saved = settings.data.saved;
  const busy = change.isPending ? t("settings.saving") : undefined;
  const overridden = saved.lowPower ? t("settings.overridden") : busy;
  const submit = (input: SettingsChange) => {
    change.submit(input);
  };
  return (
    <div ref={scroll} className="h-full min-h-0 overflow-auto p-[var(--space-5)]">
      <div className="mx-auto grid max-w-3xl gap-[var(--space-5)]">
        <ConnectionForm />
        <ConfigNotice
          path={config.data.path}
          available={config.data.available}
          opening={open.isPending}
          onOpen={() => {
            open.submit(undefined);
          }}
        />
        <section ref={appearance} className="grid gap-[var(--space-3)]">
          <h2>{t(sections[0].title)}</h2>
          <PreferenceRow label={t("settings.theme")} disabledReason={busy}>
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
          <PreferenceRow label={t("settings.lowPower")} disabledReason={busy}>
            <Switch
              checked={saved.lowPower}
              onCheckedChange={(value) => {
                submit({ field: "lowPower", value });
              }}
            />
          </PreferenceRow>
          <PreferenceRow label={t("settings.transparency")} disabledReason={overridden}>
            <Switch
              checked={saved.transparency}
              disabled={saved.lowPower || change.isPending}
              onCheckedChange={(value) => {
                submit({ field: "transparency", value });
              }}
            />
          </PreferenceRow>
          <PreferenceRow label={t("settings.reducedMotion")} disabledReason={overridden}>
            <Switch
              checked={saved.reducedMotion}
              disabled={saved.lowPower || change.isPending}
              onCheckedChange={(value) => {
                submit({ field: "reducedMotion", value });
              }}
            />
          </PreferenceRow>
        </section>
        <PreferenceRow label={t("settings.notifications")} disabledReason={busy}>
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
        <PreferenceRow label={t("settings.projectOpening")} disabledReason={busy}>
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
        {change.isPending && <Spinner />}
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
  const formatPath = usePathFormatter();
  if (path === null) return null;
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-[var(--space-2)] text-sm text-[var(--color-muted)]">
      <Info size={16} />
      <span>{t("settings.serverConfig")}</span>
      <span className="min-w-0 break-all font-mono">{formatPath(path)}</span>
      {available && (
        <DisabledInteractionGuard disabled={opening} reason={t("settings.opening")}>
          <Button variant="secondary" disabled={opening} onClick={onOpen} loading={opening}>
            {t("settings.open")}
          </Button>
        </DisabledInteractionGuard>
      )}
    </div>
  );
}
