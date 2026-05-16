import { render, screen } from "@testing-library/react";

import { App } from "./App";

describe("App", () => {
  it("renders the desktop foundation shell", () => {
    render(<App />);

    expect(screen.getByRole("heading", { name: "Builder Desktop" })).toBeInTheDocument();
    expect(screen.getByText(/remote-control surface/i)).toBeInTheDocument();
  });
});
