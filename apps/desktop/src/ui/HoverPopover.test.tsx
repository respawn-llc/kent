import { act, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { HoverPopover } from "./HoverPopover";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "./radix/tooltip";

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

async function advanceTime(duration: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(duration);
  });
}

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
  render(<Example />);
  const trigger = screen.getByRole("button", { name: "Trigger" });
  trigger.focus();
  expect(screen.queryByRole("button", { name: "Popup action" })).not.toBeInTheDocument();
  fireEvent.mouseEnter(trigger);
  const action = screen.getByRole("button", { name: "Popup action" });
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
  fireEvent.click(action);
  expect(action).toBeInTheDocument();
  fireEvent.mouseLeave(document.documentElement);
  await advanceTime(250);
  expect(action).not.toBeInTheDocument();
  referenceBounds.mockRestore();
  popupBounds.mockRestore();
});

it("opens tooltips from hover but not keyboard focus", () => {
  render(
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger>Trigger</TooltipTrigger>
        <TooltipContent>Explanation</TooltipContent>
      </Tooltip>
    </TooltipProvider>,
  );
  const trigger = screen.getByRole("button");
  act(() => {
    trigger.focus();
  });
  expect(trigger).toHaveFocus();
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  fireEvent.mouseEnter(trigger);
  expect(screen.getByRole("tooltip")).toBeInTheDocument();
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
  render(<Example />);
  const trigger = screen.getByRole("button", { name: "Settings" });
  fireEvent.mouseEnter(trigger);
  const popup = screen.getByRole("dialog");
  vi.spyOn(trigger, "getBoundingClientRect").mockReturnValue(new DOMRect(100, 100, 100, 40));
  vi.spyOn(popup, "getBoundingClientRect").mockReturnValue(new DOMRect(100, 148, 320, 200));
  fireEvent.mouseLeave(trigger, { clientX: 150, clientY: 140 });
  fireEvent.mouseMove(popup, { clientX: 200, clientY: 180 });
  const nestedTrigger = screen.getByRole("button", { name: "Session ID" });
  fireEvent.mouseEnter(nestedTrigger, { clientX: 400, clientY: 160 });
  const tooltip = screen.getByRole("tooltip");
  vi.spyOn(nestedTrigger, "getBoundingClientRect").mockReturnValue(new DOMRect(380, 148, 40, 28));
  vi.spyOn(tooltip, "getBoundingClientRect").mockReturnValue(new DOMRect(350, 100, 100, 40));
  expect(popup.contains(tooltip)).toBe(false);
  fireEvent.mouseLeave(nestedTrigger, { clientX: 400, clientY: 148 });
  fireEvent.mouseLeave(popup, { clientX: 400, clientY: 148 });
  fireEvent.mouseMove(document, { clientX: 400, clientY: 144 });
  fireEvent.mouseEnter(tooltip, { clientX: 400, clientY: 138 });
  fireEvent.mouseMove(tooltip, { clientX: 400, clientY: 120 });
  await advanceTime(250);
  expect(popup).toBeInTheDocument();
  expect(tooltip).toBeInTheDocument();
  fireEvent.mouseLeave(tooltip, { clientX: 450, clientY: 120 });
  fireEvent.mouseMove(document, { clientX: 600, clientY: 450 });
  await advanceTime(250);
  expect(popup).not.toBeInTheDocument();
  expect(tooltip).not.toBeInTheDocument();
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
  render(<Example />);
  const trigger = screen.getByRole("button", { name: "Context" });
  fireEvent.mouseEnter(trigger);
  const popup = screen.getByRole("dialog");
  const referenceBounds = vi
    .spyOn(trigger, "getBoundingClientRect")
    .mockReturnValue(new DOMRect(400, 400, 60, 28));
  const popupBounds = vi
    .spyOn(popup, "getBoundingClientRect")
    .mockReturnValue(new DOMRect(140, 180, 320, 212));
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
    referenceBounds.mockRestore();
    popupBounds.mockRestore();
  }
});

it("preserves asChild click behavior and dismisses standalone tooltips with Escape and outside press", async () => {
  const click = vi.fn();
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
  fireEvent.mouseEnter(trigger);
  expect(screen.getByRole("tooltip")).toBeInTheDocument();
  const action = screen.getByRole("button", { name: "Tooltip action" });
  fireEvent.pointerDown(action);
  fireEvent.click(action);
  expect(screen.getByRole("tooltip")).toBeInTheDocument();
  fireEvent.keyDown(document, { key: "Escape" });
  await advanceTime(250);
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  fireEvent.mouseLeave(trigger);
  fireEvent.mouseEnter(trigger);
  expect(screen.getByRole("tooltip")).toBeInTheDocument();
  fireEvent.pointerDown(document.body);
  fireEvent.click(document.body);
  await advanceTime(250);
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  fireEvent.click(trigger);
  expect(click).toHaveBeenCalledOnce();
});

it("respects controlled open state and the provider hover delay", async () => {
  const view = render(
    <TooltipProvider delayDuration={100}>
      <Tooltip open={false}>
        <TooltipTrigger>Trigger</TooltipTrigger>
        <TooltipContent>Explanation</TooltipContent>
      </Tooltip>
    </TooltipProvider>,
  );
  const trigger = screen.getByRole("button");
  fireEvent.mouseEnter(trigger);
  await advanceTime(150);
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  fireEvent.mouseLeave(trigger);
  view.rerender(
    <TooltipProvider delayDuration={100}>
      <Tooltip>
        <TooltipTrigger>Trigger</TooltipTrigger>
        <TooltipContent>Explanation</TooltipContent>
      </Tooltip>
    </TooltipProvider>,
  );
  fireEvent.mouseEnter(trigger);
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  await advanceTime(99);
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  await advanceTime(1);
  expect(screen.getByRole("tooltip")).toBeInTheDocument();
});
