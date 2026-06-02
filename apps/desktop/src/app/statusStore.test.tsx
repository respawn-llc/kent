import { fireEvent, render, screen, within } from "@testing-library/react";

import { StatusProvider } from "./statusStore";
import { useStatusController } from "./useStatusController";

describe("StatusProvider test surface", () => {
  it("renders title-only notices without body text", async () => {
    render(
      <StatusProvider>
        <TitleOnlyNoticeButton />
      </StatusProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Show" }));

    const surface = screen.getByTestId("sonner-test-surface");
    expect(await within(surface).findByText("Copied", { selector: "strong" })).toBeInTheDocument();
    expect(within(surface).queryByText("Copied", { selector: "p" })).not.toBeInTheDocument();
  });

  it("renders empty-body notices without body text", async () => {
    render(
      <StatusProvider>
        <EmptyBodyNoticeButton />
      </StatusProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Show" }));

    const surface = screen.getByTestId("sonner-test-surface");
    expect(await within(surface).findByText("Copied", { selector: "strong" })).toBeInTheDocument();
    expect(within(surface).queryByText("Copied", { selector: "p" })).not.toBeInTheDocument();
  });
});

function TitleOnlyNoticeButton() {
  const { push } = useStatusController();
  return (
    <button
      onClick={() => {
        push({
          id: "title-only",
          title: "Copied",
          tone: "success",
        });
      }}
      type="button"
    >
      Show
    </button>
  );
}

function EmptyBodyNoticeButton() {
  const { push } = useStatusController();
  return (
    <button
      onClick={() => {
        push({
          body: "",
          id: "empty-body",
          title: "Copied",
          tone: "success",
        });
      }}
      type="button"
    >
      Show
    </button>
  );
}
