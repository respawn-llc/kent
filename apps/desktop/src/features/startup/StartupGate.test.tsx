import { render, screen } from "@testing-library/react";

import { App } from "../../App";
import { StartupConfigurationError } from "../../api/errors";
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

    expect(await screen.findByRole("heading", { name: "Builder service unreachable" }, { timeout: 4_000 })).toBeInTheDocument();
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
              protocol_version: "2",
              auth_ready: false,
              auth_required: true,
              endpoint: "ws://127.0.0.1:53082/rpc",
              causes: [{ code: "auth", severity: "error", summary: "Auth required", next_action: "Run builder auth login", diagnostic_id: "diag-1" }],
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
          { method: "server.readiness.get", result: {}, error: new StartupConfigurationError("invalid Builder config") },
        ])}
      />,
    );

    expect(await screen.findByRole("heading", { name: "Startup blocked" }, { timeout: 4_000 })).toBeInTheDocument();
    expect(screen.getByText("invalid Builder config")).toBeInTheDocument();
    expect(screen.queryByText("Run `builder service install`, then retry.")).not.toBeInTheDocument();
  });

  it("blocks app when required capability is unavailable", async () => {
    render(
      <App
        services={createTestServices([
          startupRoutes[0] ?? failTest("readiness route missing"),
          {
            method: "server.capabilities.get",
            result: {
              capabilities: [
                { id: "workflow.board", available: false, reason: "workflow board disabled", required_for_mvp: true },
              ],
              server_version: "1.3.0",
              protocol_version: "2",
            },
          },
        ])}
      />,
    );

    expect(await screen.findByRole("heading", { name: "Required capability unavailable" })).toBeInTheDocument();
    expect(screen.getByText("workflow board disabled")).toBeInTheDocument();
  });
});

function failTest(message: string): never {
  throw new Error(message);
}
