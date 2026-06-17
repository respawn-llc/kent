import { useState } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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

  it("traps keyboard focus, closes with Escape, and restores trigger focus", async () => {
    const user = userEvent.setup();
    render(<DialogHarness />);

    const opener = screen.getByRole("button", { name: "Open dialog" });
    await user.click(opener);

    const close = screen.getByRole("button", { name: "Close dialog" });
    const cancel = screen.getByRole("button", { name: "Cancel" });
    const deleteButton = screen.getByRole("button", { name: "Delete" });
    await waitFor(() => {
      expect(close).toHaveFocus();
    });

    await user.tab();
    expect(cancel).toHaveFocus();
    await user.tab();
    expect(deleteButton).toHaveFocus();
    await user.tab();
    expect(close).toHaveFocus();
    await user.tab({ shift: true });
    expect(deleteButton).toHaveFocus();

    await user.keyboard("{Escape}");

    await waitFor(() => {
      expect(screen.queryByRole("dialog", { name: "Danger dialog" })).not.toBeInTheDocument();
    });
    expect(opener).toHaveFocus();
  });

  it("preserves controlled textarea focus while the close callback identity changes", async () => {
    const user = userEvent.setup();
    render(<ControlledTextareaDialogHarness />);

    const input = screen.getByRole("textbox", { name: "Missing value" });
    await user.click(input);
    await user.type(input, "abc");

    expect(input).toHaveValue("abc");
    expect(input).toHaveFocus();
  });
});

function DialogHarness() {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button
        onClick={() => {
          setOpen(true);
        }}
        type="button"
      >
        Open dialog
      </button>
      <Dialog
        closeLabel="Close dialog"
        onClose={() => {
          setOpen(false);
        }}
        open={open}
        title="Danger dialog"
      >
        <button type="button">Cancel</button>
        <button type="button">Delete</button>
      </Dialog>
    </>
  );
}

function ControlledTextareaDialogHarness() {
  const [value, setValue] = useState("");
  return (
    <Dialog
      closeLabel="Close dialog"
      onClose={() => {
        setValue("");
      }}
      open
      title="Controlled dialog"
    >
      <textarea
        aria-label="Missing value"
        onChange={(event) => {
          setValue(event.currentTarget.value);
        }}
        value={value}
      />
    </Dialog>
  );
}
