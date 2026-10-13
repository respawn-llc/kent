import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { HoverPopover } from "./HoverPopover";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "./radix/tooltip";

it("opens on hover, keeps interactive content open on click, and closes on leaving", async () => {
  function Example() {
    const [open, setOpen] = useState(false);
    return (
      <HoverPopover
        open={open}
        onOpenChange={setOpen}
        trigger={(props) => <button {...props}>Trigger</button>}
      >
        <button>Popup action</button>
      </HoverPopover>
    );
  }
  const user = userEvent.setup({ skipHover: true });
  render(<Example />);
  const trigger = screen.getByRole("button", { name: "Trigger" });
  trigger.focus();
  expect(screen.queryByRole("button", { name: "Popup action" })).not.toBeInTheDocument();
  await user.hover(trigger);
  const action = await screen.findByRole("button", { name: "Popup action" });
  const popup = screen.getByRole("dialog");
  const referenceBounds = vi
    .spyOn(trigger, "getBoundingClientRect")
    .mockReturnValue(new DOMRect(100, 100, 100, 40));
  const popupBounds = vi
    .spyOn(popup, "getBoundingClientRect")
    .mockReturnValue(new DOMRect(100, 148, 320, 200));
  fireEvent.mouseLeave(trigger, { clientX: 150, clientY: 140 });
  fireEvent.mouseMove(document, { clientX: 151, clientY: 144 });
  expect(action).toBeInTheDocument();
  fireEvent.mouseEnter(action);
  await user.click(action);
  expect(action).toBeInTheDocument();
  fireEvent.mouseLeave(document.documentElement);
  await waitFor(() => {
    expect(action).not.toBeInTheDocument();
  });
  referenceBounds.mockRestore();
  popupBounds.mockRestore();
});

it("opens tooltips from hover but not keyboard focus", async () => {
  const user = userEvent.setup();
  render(
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger>Trigger</TooltipTrigger>
        <TooltipContent>Explanation</TooltipContent>
      </Tooltip>
    </TooltipProvider>,
  );
  const trigger = screen.getByRole("button");
  await user.tab();
  expect(trigger).toHaveFocus();
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  await user.hover(trigger);
  expect(await screen.findByRole("tooltip")).toBeInTheDocument();
});
