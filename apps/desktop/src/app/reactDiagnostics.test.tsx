import { render } from "@testing-library/react";
import { Component, type ReactElement } from "react";

import { createTestServices } from "@/test-support/app-services";
import { createDevelopmentReactErrorHandlers } from "./reactDiagnostics";

it("preserves a caught render exception and its component stack after boundary recovery", async () => {
  const { logger } = createTestServices([]);
  const report = vi.fn();
  vi.stubGlobal("reportError", report);
  const failure = new Error("Render failure");
  let recovered = false;
  class Boundary extends Component<{ content: ReactElement }, { failed: boolean }> {
    override state = { failed: false };
    static getDerivedStateFromError() {
      return { failed: true };
    }
    override render(): ReactElement | null {
      return this.state.failed ? null : this.props.content;
    }
  }
  function FailingMessage() {
    if (!recovered) throw failure;
    return <div />;
  }
  const handlers = createDevelopmentReactErrorHandlers(logger);
  const view = render(<Boundary key="failed" content={<FailingMessage />} />, {
    onCaughtError: handlers.onCaughtError,
    onRecoverableError: handlers.onRecoverableError,
  });
  try {
    recovered = true;
    view.rerender(<Boundary key="recovered" content={<FailingMessage />} />);
    expect(logger.entries()).toHaveLength(1);
    expect(logger.entries()[0]).toMatchObject({
      level: "error",
      context: { kind: "caught", stack: failure.stack },
    });
    expect(typeof logger.entries()[0]?.context.componentStack).toBe("string");
    expect(logger.entries()[0]?.context.componentStack?.length).toBeGreaterThan(0);
    expect(report).toHaveBeenCalledWith(failure);
  } finally {
    view.unmount();
    vi.unstubAllGlobals();
  }
});
