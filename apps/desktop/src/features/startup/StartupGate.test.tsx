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

    expect(
      await screen.findByRole("heading", { name: "Builder service unreachable" }, { timeout: 4_000 }),
    ).toBeInTheDocument();
    expect(screen.getByText("connection refused")).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Projects" })).not.toBeInTheDocument();
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
                  next_action: "Run builder auth login",
                  diagnostic_id: "diag-1",
                },
              ],
            },
          },
        ])}
      />,
    );

    expect(await screen.findByRole("heading", { name: "Startup blocked" })).toBeInTheDocument();
    expect(screen.getByText("Auth required Run builder auth login")).toBeInTheDocument();
    expect(screen.queryByText("Run `builder service install`, then retry.")).not.toBeInTheDocument();
  });

  it("surfaces native configuration errors without service-install guidance", async () => {
    render(
      <App
        services={createTestServices([
          {
            method: "server.readiness.get",
            result: {},
            error: new StartupConfigurationError("invalid Builder config"),
          },
        ])}
      />,
    );

    expect(
      await screen.findByRole("heading", { name: "Startup blocked" }, { timeout: 4_000 }),
    ).toBeInTheDocument();
    expect(screen.getByText("invalid Builder config")).toBeInTheDocument();
    expect(screen.queryByText("Run `builder service install`, then retry.")).not.toBeInTheDocument();
  });

  it("does not call removed backend capabilities route before showing app content", async () => {
    const services = createTestServices(startupRoutes);

    render(
      <App services={services} />,
    );

    expect(await screen.findByRole("heading", { name: "Projects" })).toBeInTheDocument();
    expect(services.transport.calls.map((call) => call.method)).not.toContain("server.capabilities.get");
  });

  it("keeps disconnected status non-dismissible until reconnect", async () => {
    const services = createTestServices(startupRoutes);

    render(<App services={services} />);

    expect(await screen.findByRole("heading", { name: "Projects" })).toBeInTheDocument();

    act(() => {
      services.transport.connection.set("disconnected", "closed");
    });

    expect(await screen.findByText("Server disconnected. Cached data remains visible; mutations are disabled.")).toBeInTheDocument();
    expect(screen.getByText("Reconnecting")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Close" })).not.toBeInTheDocument();

    act(() => {
      services.transport.connection.set("connected");
    });

    await waitFor(() => {
      expect(screen.queryByText("Server disconnected. Cached data remains visible; mutations are disabled.")).not.toBeInTheDocument();
    });
  });

  it("blocks feature surfaces when server protocol does not match desktop protocol", async () => {
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

    expect(await screen.findByRole("heading", { name: "Update Builder" })).toBeInTheDocument();
    expect(screen.getByText(new RegExp(`Client protocol ${protocolVersion}`, "u"))).toBeInTheDocument();
    expect(screen.getByText(/Server protocol 1/)).toBeInTheDocument();
    expect(
      screen.getByText(/Update Builder CLI\/service and desktop app from the same build/),
    ).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Projects" })).not.toBeInTheDocument();
  });
});
