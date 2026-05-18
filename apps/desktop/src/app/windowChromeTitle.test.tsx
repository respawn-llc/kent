import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";

import {
  useCurrentWindowChromeTitle,
  useWindowChromeTitle,
  WindowChromeTitleProvider,
} from "./windowChromeTitle";

describe("window chrome title", () => {
  it("sets the current destination title from a one-line hook call", () => {
    render(
      <WindowChromeTitleProvider>
        <TitleReader />
        <TitleSetter title="Static title" />
      </WindowChromeTitleProvider>,
    );

    expect(screen.getByRole("status")).toHaveTextContent("Static title");
  });

  it("updates the title asynchronously when destination content loads", () => {
    render(
      <WindowChromeTitleProvider>
        <TitleReader />
        <AsyncTitleSetter />
      </WindowChromeTitleProvider>,
    );

    expect(screen.getByRole("status")).toHaveTextContent("no title");
    fireEvent.click(screen.getByRole("button", { name: "Load title" }));
    expect(screen.getByRole("status")).toHaveTextContent("Loaded title");
  });

  it("clears an existing title when a destination explicitly sets null", () => {
    const { rerender } = render(
      <WindowChromeTitleProvider>
        <TitleReader />
        <TitleSetter title="Previous title" />
      </WindowChromeTitleProvider>,
    );

    expect(screen.getByRole("status")).toHaveTextContent("Previous title");

    rerender(
      <WindowChromeTitleProvider>
        <TitleReader />
        <TitleSetter title={null} />
      </WindowChromeTitleProvider>,
    );

    expect(screen.getByRole("status")).toHaveTextContent("no title");
  });
});

function TitleSetter({ title }: Readonly<{ title: string | null }>) {
  useWindowChromeTitle(title);
  return null;
}

function AsyncTitleSetter() {
  const [title, setTitle] = useState<string | null>(null);
  useWindowChromeTitle(title);

  return (
    <button
      onClick={() => {
        setTitle("Loaded title");
      }}
      type="button"
    >
      Load title
    </button>
  );
}

function TitleReader() {
  const title = useCurrentWindowChromeTitle();
  return <p role="status">{title ?? "no title"}</p>;
}
