import { act, render, screen, waitFor } from "@testing-library/react";

import { App } from "../../App";
import { StartupConfigurationError } from "../../api/errors";
import { protocolVersion } from "../../api/jsonRpcSocket";
import { createTestServices, startupRoutes } from "../../testSupport/appServices";

describe("StartupGate", () => {
  it("surfaces unavailable server errors before showing app content", async () => {
    render(
      <App
        services={createTestServices([
          { method: "server.readiness.get", result: {}, error: new Error("connection refused") },
        ])}
      />,
    );

    expect(await screen.findByTestId("error-state", undefined, { timeout: 4_000 })).toBeInTheDocument();
    expect(screen.queryByTestId("home-route-root")).not.toBeInTheDocument();
  });

  it("surfaces not-ready auth and startup causes", async () => {
    render(
      <App
        services={createTestServices([
          {
            method: "server.readiness.get",
            result: {
              ready: false,
              server_id: "server-1",
              server_version: "1.3.0",
              protocol_version: protocolVersion,
              auth_ready: false,
              auth_required: true,
              endpoint: "ws://127.0.0.1:53082/rpc",
              causes: [
                {
                  code: "auth",
                  severity: "error",
                  summary: "Auth required",
                  next_action: "Run kent auth login",
                  diagnostic_id: "diag-1",
                },
              ],
            },
          },
        ])}
      />,
    );

    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
  });

  it("surfaces native configuration errors without service-install guidance", async () => {
    render(
      <App
        services={createTestServices([
          {
            method: "server.readiness.get",
            result: {},
            error: new StartupConfigurationError("invalid Kent config"),
          },
        ])}
      />,
    );

    expect(await screen.findByTestId("error-state", undefined, { timeout: 4_000 })).toBeInTheDocument();
  });

  it("does not call removed backend capabilities route before showing app content", async () => {
    const services = createTestServices(startupRoutes);

    render(
      <App services={services} />,
    );

    expect(await screen.findByTestId("home-route-root")).toBeInTheDocument();
    expect(services.transport.calls.map((call) => call.method)).not.toContain("server.capabilities.get");
  });

  it("keeps disconnected status non-dismissible until reconnect", async () => {
    const services = createTestServices(startupRoutes);

    render(<App services={services} />);

    expect(await screen.findByTestId("home-route-root")).toBeInTheDocument();
    const readinessCallsBeforeReconnect = services.transport.calls.filter(
      (call) => call.method === "server.readiness.get",
    ).length;

    act(() => {
      services.transport.connection.set("disconnected", "closed");
    });

    expect(screen.queryByRole("button", { name: "Close" })).not.toBeInTheDocument();

    act(() => {
      services.transport.connection.set("connected");
    });

    await waitFor(() => {
      expect(services.transport.calls.filter((call) => call.method === "server.readiness.get").length).toBe(
        readinessCallsBeforeReconnect + 1,
      );
    });
  });

  it("resets to startup when server protocol does not match desktop protocol", async () => {
    window.history.pushState(null, "", "/workflows/workflow-1/editor");
    render(
      <App
        services={createTestServices([
          {
            method: "server.readiness.get",
            result: {
              ready: true,
              server_id: "server-1",
              server_version: "1.3.0",
              protocol_version: "1",
              auth_ready: true,
              auth_required: true,
              endpoint: "ws://127.0.0.1:53082/rpc",
            },
          },
        ])}
      />,
    );

    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
    expect(
      screen.getByText(
        `Client protocol ${protocolVersion}. Server protocol 1. Update Kent CLI/service and desktop app from the same build.`,
      ),
    ).toBeInTheDocument();
    await waitFor(() => {
      expect(window.location.pathname).toBe("/");
    });
    expect(screen.queryByTestId("home-route-root")).not.toBeInTheDocument();
  });
});
