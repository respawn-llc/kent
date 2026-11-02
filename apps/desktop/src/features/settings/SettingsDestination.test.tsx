import { expect, it, onTestFinished, vi } from "bun:test";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { appI18n } from "@/i18n";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { SettingsDestination } from "./SettingsDestination";

it("keeps unrelated switches enabled and checked while another preference saves", async () => {
  const base = createTestServices([]);
  const saved = { ...base.initialSettings.saved, transparency: true, reducedMotion: true };
  const services = { ...base, initialSettings: { ...base.initialSettings, saved } };
  let complete!: (value: typeof saved) => void;
  const change = vi.spyOn(services.nativeBridge.settings, "change").mockImplementation(
    async () =>
      new Promise((resolve) => {
        complete = resolve;
      }),
  );
  onTestFinished(() => {
    change.mockRestore();
  });
  render(
    <TestAppProviders services={services}>
      <SettingsDestination active configPath={null}>
        {({ page }) => <>{page}</>}
      </SettingsDestination>
    </TestAppProviders>,
  );
  const transparency = await screen.findByRole("switch", { name: appI18n.t("settings.transparency") });
  const reducedMotion = screen.getByRole("switch", { name: appI18n.t("settings.reducedMotion") });
  const transitions: (string | null)[] = [];
  const observer = new MutationObserver((records) => {
    for (const record of records) {
      if (record.attributeName === "aria-checked" && record.target instanceof HTMLElement) {
        transitions.push(record.target.getAttribute("aria-checked"));
      }
    }
  });
  observer.observe(transparency, { attributes: true });
  observer.observe(reducedMotion, { attributes: true });
  try {
    fireEvent.click(screen.getByRole("radio", { name: appI18n.t("settings.light") }));
    await waitFor(() => {
      expect(change).toHaveBeenCalledWith({ field: "theme", value: "light" });
    });
    expect(transparency).toBeChecked();
    expect(reducedMotion).toBeChecked();
    expect(transparency).not.toBeDisabled();
    expect(reducedMotion).not.toBeDisabled();
    fireEvent.click(transparency);
    expect(change).toHaveBeenCalledTimes(1);
    await act(async () => {
      complete({ ...saved, theme: "light" });
    });
    await waitFor(() => {
      expect(screen.getByRole("radio", { name: appI18n.t("settings.light") })).toBeChecked();
    });
    expect(screen.getByRole("switch", { name: appI18n.t("settings.transparency") })).toBeChecked();
    expect(screen.getByRole("switch", { name: appI18n.t("settings.reducedMotion") })).toBeChecked();
    expect(transitions.every((value) => value === "true")).toBe(true);
  } finally {
    observer.disconnect();
  }
});
