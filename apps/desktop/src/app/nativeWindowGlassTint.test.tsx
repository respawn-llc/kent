import { act, render, waitFor } from "@testing-library/react";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { createBrowserNativeBridge } from "@/test-support/native-bridge";
import { useNativeWindowGlassTintSync } from "./nativeWindowGlassTint";

it("stops theme-driven tint delivery when the window owner leaves", async () => {
  const bridge = createBrowserNativeBridge({ platform: "macos" });
  const apply = vi.spyOn(bridge.window, "setCurrentGlassTint");
  function Owner() {
    useNativeWindowGlassTintSync(bridge);
    return null;
  }
  const view = render(
    <TestAppProviders services={createTestServices([], bridge)}>
      <Owner />
    </TestAppProviders>,
  );
  await waitFor(() => {
    expect(apply).toHaveBeenCalledTimes(1);
  });
  await act(async () => {
    document.documentElement.setAttribute("data-theme", "dark");
  });
  await waitFor(() => {
    expect(apply).toHaveBeenCalledTimes(2);
  });
  view.unmount();
  await act(async () => {
    document.documentElement.setAttribute("data-theme", "light");
  });
  expect(apply).toHaveBeenCalledTimes(2);
  document.documentElement.removeAttribute("data-theme");
});
