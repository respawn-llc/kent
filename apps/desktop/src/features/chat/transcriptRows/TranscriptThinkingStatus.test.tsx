import { beforeAll, expect, it } from "bun:test";
import { act, render, screen, waitFor } from "@testing-library/react";
import { MotionConfig } from "motion/react";
import type { ReactNode } from "react";
import { useWindowFocus } from "@/app-facade";
import { initializeI18n } from "@/i18n";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { createBrowserNativeBridge } from "@/test-support/native-bridge";
import { ReducedMotionContext } from "@/ui";
import { TranscriptThinkingStatus } from "./TranscriptThinkingStatus";

beforeAll(initializeI18n);

function status(text: string | null, reduced = false) {
  return (
    <ReducedMotionContext value={reduced}>
      <TranscriptThinkingStatus presentation={text === null ? null : { kind: "text", text }} />
    </ReducedMotionContext>
  );
}

function FocusState() {
  return <output data-testid="focus-state">{String(useWindowFocus())}</output>;
}

function renderStatus(content: ReactNode) {
  const services = createTestServices([], createBrowserNativeBridge());
  return render(content, {
    wrapper: ({ children }) => (
      <TestAppProviders services={services}>
        <FocusState />
        {children}
      </TestAppProviders>
    ),
  });
}

it("keeps active status on blur and removes it immediately when activity ends unfocused", async () => {
  const view = renderStatus(status("active work"));
  await act(async () => {
    window.dispatchEvent(new Event("focus"));
  });
  await waitFor(() => expect(screen.getByTestId("focus-state")).toHaveTextContent("true"));
  await act(async () => {
    window.dispatchEvent(new Event("blur"));
  });
  await waitFor(() => expect(screen.getByTestId("focus-state")).toHaveTextContent("false"));
  expect(screen.getByText("active work")).toBeVisible();

  view.rerender(status(null));
  expect(screen.queryByText("active work")).toBeNull();

  await act(async () => {
    window.dispatchEvent(new Event("focus"));
  });
  await waitFor(() => expect(screen.getByTestId("focus-state")).toHaveTextContent("true"));
  expect(screen.queryByText("active work")).not.toBeInTheDocument();
});

it("retires replaced status text after the transition completes", async () => {
  const view = renderStatus(<MotionConfig skipAnimations>{status("first activity")}</MotionConfig>);
  view.rerender(<MotionConfig skipAnimations>{status("second activity")}</MotionConfig>);
  expect(screen.getByText("second activity")).toBeInTheDocument();
  await waitFor(() => expect(screen.queryByText("first activity")).not.toBeInTheDocument());
  expect(screen.getByText("second activity")).toBeVisible();
});

it("replaces status text immediately with reduced motion", () => {
  const view = renderStatus(status("first activity", true));
  view.rerender(status("second activity", true));
  expect(screen.queryByText("first activity")).not.toBeInTheDocument();
  expect(screen.getByText("second activity")).toBeVisible();
});

it("retires outgoing status text when reduced motion is enabled mid-transition", () => {
  const view = renderStatus(status("first activity"));
  view.rerender(status("second activity"));
  expect(screen.getByText("first activity")).toBeInTheDocument();
  view.rerender(status("second activity", true));
  expect(screen.queryByText("first activity")).not.toBeInTheDocument();
  expect(screen.getByText("second activity")).toBeVisible();
});
