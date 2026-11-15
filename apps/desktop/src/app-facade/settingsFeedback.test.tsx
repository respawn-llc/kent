import { afterEach, expect, it, vi } from "bun:test";
import { waitFor } from "@testing-library/react";
import { createInstance } from "i18next";
import { createTestServices } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import type { StatusNotice } from "@/ui";
import { requestSettingsRestart } from "./settingsFeedback";

afterEach(() => {
  vi.restoreAllMocks();
});

it.each([false, true])(
  "dismisses the restart notice while relaunch is pending (failure: %s)",
  async (fail) => {
    const services = createTestServices([]);
    const pending = deferred<undefined>();
    const relaunch = vi.spyOn(services.nativeBridge.updates, "relaunch").mockReturnValue(pending.promise);
    const status = { push: vi.fn<(notice: StatusNotice) => void>(), dismiss: vi.fn<(id: string) => void>() };
    requestSettingsRestart(
      {
        ...services.initialSettings,
        saved: {
          ...services.initialSettings.saved,
          transparency: !services.initialSettings.saved.transparency,
        },
      },
      services,
      status,
      createInstance().t,
    );
    const notice = status.push.mock.calls[0]?.[0];
    expect(notice?.onAction).toBeDefined();
    notice?.onAction?.();
    expect(status.dismiss).toHaveBeenCalledWith(notice?.id);
    expect(relaunch).toHaveBeenCalledTimes(1);
    expect(status.push).toHaveBeenCalledTimes(1);
    if (fail) {
      pending.reject(new Error("relaunch failed"));
      await waitFor(() => {
        expect(status.push).toHaveBeenCalledTimes(2);
      });
      expect(status.push.mock.calls[1]?.[0].tone).toBe("danger");
    } else {
      pending.resolve(undefined);
      await pending.promise;
      expect(status.push).toHaveBeenCalledTimes(1);
    }
  },
);
