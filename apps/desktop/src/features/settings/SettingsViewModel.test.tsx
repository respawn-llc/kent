import { expect, it, vi } from "bun:test";
import { RegistryProvider, useAtomValue } from "@effect/atom-react";
import { QueryClient } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { createTestServices } from "@/test-support/app-services";
import { appI18n } from "@/i18n";
import { createSettingsViewModel } from "./SettingsViewModel";

it.each([true, false])(
  "offers the exact server-reported local config when available=%s, even on a remote endpoint",
  async (available) => {
    const base = createTestServices([]);
    const services = { ...base, endpoint: "wss://remote.example/rpc" };
    const path = "/server/config/config.toml";
    const availability = vi.spyOn(services.nativeBridge.files, "fileAvailable").mockResolvedValue(available);
    const client = new QueryClient();
    const model = createSettingsViewModel({
      services,
      client,
      configPath: path,
      status: { push: vi.fn(), dismiss: vi.fn() },
      t: appI18n.t,
    });
    const view = renderHook(() => useAtomValue(model.config), {
      wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider>,
    });
    await waitFor(() => {
      expect(view.result.current.isSuccess).toBe(true);
    });
    expect(view.result.current.data).toEqual({ path, available });
    expect(availability).toHaveBeenCalledWith({ path });
    view.unmount();
    client.clear();
  },
);
