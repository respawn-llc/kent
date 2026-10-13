import { render, waitFor } from "@testing-library/react";
import { useWindowChromeTitle } from "@/app-facade";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { WindowChromeFrame } from "./WindowChromeFrame";

it("updates the operating-system window title when the conversation title changes", async () => {
  const services = createTestServices([]);
  function Chat({ title }: Readonly<{ title: string }>) {
    useWindowChromeTitle(title);
    return <WindowChromeFrame>{null}</WindowChromeFrame>;
  }
  const view = render(
    <TestAppProviders services={services}>
      <Chat title="First conversation" />
    </TestAppProviders>,
  );
  await waitFor(() => expect(document.title).toBe("First conversation"));
  view.rerender(
    <TestAppProviders services={services}>
      <Chat title="Renamed conversation" />
    </TestAppProviders>,
  );
  await waitFor(() => expect(document.title).toBe("Renamed conversation"));
});

it("keeps an unnamed detached conversation identifiable in the native window list", async () => {
  const services = createTestServices([]);
  render(
    <TestAppProviders services={services}>
      <WindowChromeFrame defaultTitle="session-identity">{null}</WindowChromeFrame>
    </TestAppProviders>,
  );
  await waitFor(() => expect(document.title).toBe("session-identity"));
});

it("logs native title failures without preventing the window from rendering", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.nativeBridge.window, "setTitle").mockRejectedValue(new Error("native failure"));
  const view = render(
    <TestAppProviders services={services}>
      <WindowChromeFrame>{<button>Content action</button>}</WindowChromeFrame>
    </TestAppProviders>,
  );
  await waitFor(() => expect(services.logger.entries()).toHaveLength(1));
  expect(view.getByRole("button")).toBeEnabled();
});
