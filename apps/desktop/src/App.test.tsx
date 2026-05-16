import { render, screen } from "@testing-library/react";

import { App } from "./App";
import { createTestServices, startupRoutes } from "./testSupport/appServices";

describe("App", () => {
  it("renders the startup-gated home shell", async () => {
    render(<App services={createTestServices(startupRoutes)} />);

    expect(await screen.findByRole("heading", { name: "Projects" })).toBeInTheDocument();
    expect(screen.getByText("Workflow remote control")).toBeInTheDocument();
  });
});
