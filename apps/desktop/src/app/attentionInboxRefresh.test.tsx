import { act, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "bun:test";
import { toast } from "sonner";
import { createBrowserNativeBridge } from "@app/native-bridge";
import {
  attentionEventsFixture,
  globalAttentionPage,
  globalAttentionRequests,
  globalAttentionRoute,
  questionAttentionItem,
} from "@/test-support/attention";
import { removeBrowserStorage } from "@/app-facade";
import { createTestServices, startupRoutes } from "@/test-support/app-services";
import { installAnimationFrameTestSupport } from "@/test-support/scheduling";
import { AppRoot } from "./AppRoot";

let restoreAnimationFrameTestSupport: () => void;

describe("open Inbox attention refresh", () => {
  beforeEach(() => {
    window.history.replaceState(null, "", "/");
    clearRoutePersistence();
    restoreAnimationFrameTestSupport = installAnimationFrameTestSupport();
  });

  afterEach(() => {
    restoreAnimationFrameTestSupport();
    window.history.replaceState(null, "", "/");
    clearRoutePersistence();
  });

  it("shows newly arrived authoritative attention without navigation or manual refresh", async () => {
    const services = createTestServices([
      ...startupRoutes,
      globalAttentionRoute((_token, callIndex) =>
        globalAttentionPage(callIndex === 0 ? [] : [questionAttentionItem]),
      ),
    ]);
    render(<AppRoot services={services} />);
    await waitFor(() => {
      expect(screen.getByTestId("home-route-root")).toBeInTheDocument();
      expect(globalAttentionRequests(services.transport)).toHaveLength(1);
      expect(screen.queryByTestId("attention-row")).not.toBeInTheDocument();
    });
    act(() => {
      attentionEventsFixture(services.transport).pendingTaskQuestion();
    });
    await waitFor(() => {
      expect(globalAttentionRequests(services.transport)).toHaveLength(2);
      expect(screen.getByTestId("attention-row")).toBeInTheDocument();
    });
  });

  it("removes resolved attention while Inbox remains open", async () => {
    const services = createTestServices([
      ...startupRoutes,
      globalAttentionRoute((_token, callIndex) =>
        globalAttentionPage(callIndex === 0 ? [questionAttentionItem] : []),
      ),
    ]);
    render(<AppRoot services={services} />);
    await waitFor(() => {
      expect(screen.getByTestId("attention-row")).toBeInTheDocument();
      expect(globalAttentionRequests(services.transport)).toHaveLength(1);
    });
    act(() => {
      attentionEventsFixture(services.transport).resolveQuestion();
    });
    await waitFor(() => {
      expect(globalAttentionRequests(services.transport)).toHaveLength(2);
      expect(screen.queryByTestId("attention-row")).not.toBeInTheDocument();
    });
  });

  it("delivers Session Chat questions to native notifications", async () => {
    const nativeBridge = createBrowserNativeBridge();
    vi.spyOn(nativeBridge.window, "isFocused").mockResolvedValue(false);
    const services = createTestServices(
      [...startupRoutes, globalAttentionRoute(() => globalAttentionPage([]))],
      nativeBridge,
    );
    const notify = vi.spyOn(services.nativeBridge.notifications, "notify").mockResolvedValue(undefined);
    render(<AppRoot services={services} />);
    await waitFor(() => {
      expect(screen.getByTestId("home-route-root")).toBeInTheDocument();
      expect(attentionEventsFixture(services.transport).activeCount).toBeGreaterThan(0);
    });
    const notices = toast.getToasts();
    await act(async () => {
      window.dispatchEvent(new Event("blur"));
    });
    await act(async () => {
      attentionEventsFixture(services.transport).pendingSessionQuestion();
    });
    await waitFor(() => {
      expect(notify).toHaveBeenCalledTimes(1);
    });
    expect(toast.getToasts()).toEqual(notices);
    expect(screen.queryByTestId("attention-row")).not.toBeInTheDocument();
  });
});

function clearRoutePersistence(): void {
  removeBrowserStorage("local", "desktop.lastProjectRoute");
  removeBrowserStorage("session", "desktop.routeRestoreChecked");
}
