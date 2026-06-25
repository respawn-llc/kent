import { render, screen, within } from "@testing-library/react";

import { MarkdownText } from "./MarkdownText";

describe("MarkdownText", () => {
  it("renders block markdown inside the app typography scope", () => {
    render(
      <MarkdownText
        value={"# Heading\n\n- First\n- Second\n\n> Quoted\n\n| Key | Value |\n| --- | --- |\n| A | B |"}
      />,
    );

    const markdown = screen.getByTestId("markdown-text");
    expect(markdown).toHaveClass("markdown-text");
    expect(within(markdown).getByRole("heading", { level: 1, name: "Heading" })).toBeInTheDocument();
    expect(within(markdown).getAllByRole("listitem")).toHaveLength(2);
    expect(within(markdown).getByRole("table")).toBeInTheDocument();
    expect(within(markdown).getByText("Quoted")).toBeInTheDocument();
  });

  it("keeps inline markdown inline", () => {
    render(
      <p>
        Prefix <MarkdownText inline value={"**strong** and `code`"} />
      </p>,
    );

    const markdown = screen.getByTestId("markdown-text-inline");
    expect(markdown).toHaveClass("markdown-text-inline");
    expect(screen.getByText("strong")).toBeInTheDocument();
    expect(screen.getByText("code")).toBeInTheDocument();
  });
});
