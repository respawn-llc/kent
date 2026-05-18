import { render, screen } from "@testing-library/react";
import { vi } from "vitest";

import { Dialog } from "./Dialog";

describe("Dialog", () => {
  it("uses the visible title as the accessible dialog name", () => {
    render(
      <Dialog closeLabel="Close" onClose={vi.fn()} open title="Named dialog">
        <p>Dialog body</p>
      </Dialog>,
    );

    expect(screen.getByRole("dialog", { name: "Named dialog" })).toBeInTheDocument();
  });
});
