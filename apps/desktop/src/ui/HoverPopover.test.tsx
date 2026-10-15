import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { HoverPopover } from "./HoverPopover";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "./radix/tooltip";

it.each(["click", "enter", "space", "touch"] as const)("opens without hover using %s", async (activation) => {
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
  if (activation === "click") await user.click(trigger);
  else if (activation === "touch")
    await user.pointer([
      { keys: "[TouchA>]", target: trigger },
      { keys: "[/TouchA]", target: trigger },
    ]);
  else {
    trigger.focus();
    await user.keyboard(activation === "enter" ? "{Enter}" : " ");
  }
  expect(await screen.findByRole("button", { name: "Popup action" })).toBeVisible();
  await user.keyboard("{Escape}");
  await waitFor(() => {
    expect(screen.queryByRole("button", { name: "Popup action" })).not.toBeInTheDocument();
  });
});

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

it("keeps the parent popup open through a nested portalled tooltip and closes after leaving both", async () => {
  function Example() {
    const [open, setOpen] = useState(false);
    return (
      <HoverPopover
        open={open}
        onOpenChange={setOpen}
        trigger={(props) => <button {...props}>Settings</button>}
      >
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <button>Session ID</button>
            </TooltipTrigger>
            <TooltipContent sideOffset={8}>Copy</TooltipContent>
          </Tooltip>
        </TooltipProvider>
      </HoverPopover>
    );
  }
  const user = userEvent.setup();
  render(<Example />);
  const trigger = screen.getByRole("button", { name: "Settings" });
  await user.hover(trigger);
  const popup = await screen.findByRole("dialog");
  vi.spyOn(trigger, "getBoundingClientRect").mockReturnValue(new DOMRect(100, 100, 100, 40));
  vi.spyOn(popup, "getBoundingClientRect").mockReturnValue(new DOMRect(100, 148, 320, 200));
  fireEvent.mouseLeave(trigger, { clientX: 150, clientY: 140 });
  fireEvent.mouseMove(popup, { clientX: 200, clientY: 180 });
  const nestedTrigger = screen.getByRole("button", { name: "Session ID" });
  fireEvent.mouseEnter(nestedTrigger, { clientX: 400, clientY: 160 });
  const tooltip = await screen.findByRole("tooltip");
  vi.spyOn(nestedTrigger, "getBoundingClientRect").mockReturnValue(new DOMRect(380, 148, 40, 28));
  vi.spyOn(tooltip, "getBoundingClientRect").mockReturnValue(new DOMRect(350, 100, 100, 40));
  expect(popup.contains(tooltip)).toBe(false);
  fireEvent.mouseLeave(nestedTrigger, { clientX: 400, clientY: 148 });
  fireEvent.mouseLeave(popup, { clientX: 400, clientY: 148 });
  fireEvent.mouseMove(document, { clientX: 400, clientY: 144 });
  fireEvent.mouseEnter(tooltip, { clientX: 400, clientY: 138 });
  fireEvent.mouseMove(tooltip, { clientX: 400, clientY: 120 });
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 250));
  });
  expect(popup).toBeInTheDocument();
  expect(tooltip).toBeInTheDocument();
  fireEvent.mouseLeave(tooltip, { clientX: 450, clientY: 120 });
  fireEvent.mouseMove(document, { clientX: 600, clientY: 450 });
  await waitFor(() => {
    expect(popup).not.toBeInTheDocument();
    expect(tooltip).not.toBeInTheDocument();
  });
});

it("keeps slow leftward movement inside the context popup safe area open", async () => {
  function Example() {
    const [open, setOpen] = useState(false);
    return (
      <HoverPopover
        open={open}
        onOpenChange={setOpen}
        side="top"
        align="end"
        trigger={(props) => <button {...props}>Context</button>}
      >
        <button>Compact</button>
      </HoverPopover>
    );
  }
  const user = userEvent.setup();
  render(<Example />);
  const trigger = screen.getByRole("button", { name: "Context" });
  await user.hover(trigger);
  const popup = await screen.findByRole("dialog");
  const referenceBounds = vi
    .spyOn(trigger, "getBoundingClientRect")
    .mockReturnValue(new DOMRect(400, 400, 60, 28));
  const popupBounds = vi
    .spyOn(popup, "getBoundingClientRect")
    .mockReturnValue(new DOMRect(140, 180, 320, 212));
  vi.useFakeTimers();
  try {
    fireEvent.mouseLeave(trigger, { clientX: 400, clientY: 410 });
    fireEvent.mouseMove(document, { clientX: 350, clientY: 403 });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(250);
    });
    expect(popup).toBeInTheDocument();
    fireEvent.mouseMove(document, { clientX: 349, clientY: 402 });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(250);
    });
    expect(popup).toBeInTheDocument();
    fireEvent.mouseMove(document, { clientX: 100, clientY: 450 });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(250);
    });
    expect(popup).not.toBeInTheDocument();
  } finally {
    vi.useRealTimers();
    referenceBounds.mockRestore();
    popupBounds.mockRestore();
  }
});

it("preserves asChild click behavior and dismisses standalone tooltips with Escape and outside press", async () => {
  const click = vi.fn();
  const user = userEvent.setup({ skipHover: true });
  render(
    <Tooltip>
      <TooltipTrigger asChild>
        <button onClick={click}>Copy</button>
      </TooltipTrigger>
      <TooltipContent>
        <button>Tooltip action</button>
      </TooltipContent>
    </Tooltip>,
  );
  const trigger = screen.getByRole("button", { name: "Copy" });
  await user.hover(trigger);
  await screen.findByRole("tooltip");
  const action = screen.getByRole("button", { name: "Tooltip action" });
  fireEvent.pointerDown(action);
  fireEvent.click(action);
  expect(screen.getByRole("tooltip")).toBeInTheDocument();
  await user.keyboard("{Escape}");
  await waitFor(() => {
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  });
  await user.unhover(trigger);
  await user.hover(trigger);
  await screen.findByRole("tooltip");
  await user.click(document.body);
  await waitFor(() => {
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  });
  await user.click(trigger);
  expect(click).toHaveBeenCalledOnce();
});

it("respects controlled open state and the provider hover delay", async () => {
  const user = userEvent.setup();
  const view = render(
    <TooltipProvider delayDuration={100}>
      <Tooltip open={false}>
        <TooltipTrigger>Trigger</TooltipTrigger>
        <TooltipContent>Explanation</TooltipContent>
      </Tooltip>
    </TooltipProvider>,
  );
  const trigger = screen.getByRole("button");
  await user.hover(trigger);
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 150));
  });
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  await user.unhover(trigger);
  view.rerender(
    <TooltipProvider delayDuration={100}>
      <Tooltip>
        <TooltipTrigger>Trigger</TooltipTrigger>
        <TooltipContent>Explanation</TooltipContent>
      </Tooltip>
    </TooltipProvider>,
  );
  await user.hover(trigger);
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  expect(await screen.findByRole("tooltip")).toBeInTheDocument();
});
