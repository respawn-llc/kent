import { act, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";

import { SteppedSelector } from "./SteppedSelector";

const values = ["low", "medium", "high"] as const;

function Selector({
  onCommit,
  initial = "low",
}: Readonly<{ onCommit(value: string): void; initial?: string }>) {
  const [value, setValue] = useState(initial);
  return (
    <SteppedSelector
      activeTone={() => "primary"}
      disabled={false}
      onCommit={(next) => {
        setValue(next);
        onCommit(next);
      }}
      value={value}
      values={values}
    >
      {(preview) => <output data-testid="selection">{preview}</output>}
    </SteppedSelector>
  );
}

function capturePointer(element: HTMLElement) {
  const pointers = new Set<number>();
  Object.defineProperties(element, {
    setPointerCapture: { value: (id: number) => pointers.add(id) },
    hasPointerCapture: { value: (id: number) => pointers.has(id) },
    releasePointerCapture: { value: (id: number) => pointers.delete(id) },
  });
}

it("commits changed keyboard selections and leaves repeated boundary selections alone", async () => {
  const onCommit = vi.fn();
  const user = userEvent.setup({ delay: null });
  render(<Selector onCommit={onCommit} />);
  act(() => {
    screen.getByRole("slider").focus();
  });
  await user.keyboard("{ArrowRight}");
  expect(onCommit).toHaveBeenLastCalledWith("medium");
  expect(screen.getByTestId("selection")).toHaveTextContent("medium");
  await user.keyboard("{End}{End}");
  expect(onCommit.mock.calls).toEqual([["medium"], ["high"]]);
  expect(screen.getByTestId("selection")).toHaveTextContent("high");
});

it.each([
  { end: 200, expected: "high", commits: [["high"]] },
  { end: 0, expected: "low", commits: [] },
])(
  "previews a drag ending at $expected and commits only a changed selection",
  async ({ end, expected, commits }) => {
    const bounds = vi
      .spyOn(HTMLElement.prototype, "getBoundingClientRect")
      .mockReturnValue(new DOMRect(0, 0, 200, 24));
    try {
      const onCommit = vi.fn();
      const user = userEvent.setup({ delay: null });
      render(<Selector onCommit={onCommit} />);
      const slider = screen.getByRole("slider");
      capturePointer(slider);
      await user.pointer({ target: slider, keys: "[MouseLeft>]", coords: { x: 0, y: 12 } });
      await user.pointer({ target: slider, coords: { x: 100, y: 12 } });
      expect(screen.getByTestId("selection")).toHaveTextContent("medium");
      expect(onCommit).not.toHaveBeenCalled();
      await user.pointer({ target: slider, coords: { x: end, y: 12 } });
      expect(screen.getByTestId("selection")).toHaveTextContent(expected);
      expect(onCommit).not.toHaveBeenCalled();
      await user.pointer({ target: slider, keys: "[/MouseLeft]" });
      expect(onCommit.mock.calls).toEqual(commits);
    } finally {
      bounds.mockRestore();
    }
  },
);

it("discards an interrupted drag without submitting its preview", async () => {
  const bounds = vi
    .spyOn(HTMLElement.prototype, "getBoundingClientRect")
    .mockReturnValue(new DOMRect(0, 0, 200, 24));
  try {
    const onCommit = vi.fn();
    const user = userEvent.setup({ delay: null });
    render(<Selector onCommit={onCommit} />);
    const slider = screen.getByRole("slider");
    capturePointer(slider);
    await user.pointer({ target: slider, keys: "[MouseLeft>]", coords: { x: 0, y: 12 } });
    await user.pointer({ target: slider, coords: { x: 200, y: 12 } });
    fireEvent.pointerCancel(slider);
    expect(screen.getByTestId("selection")).toHaveTextContent("low");
    expect(onCommit).not.toHaveBeenCalled();
  } finally {
    bounds.mockRestore();
  }
});

it("does not recommit a selection that became authoritative during the drag", async () => {
  const bounds = vi
    .spyOn(HTMLElement.prototype, "getBoundingClientRect")
    .mockReturnValue(new DOMRect(0, 0, 200, 24));
  try {
    const onCommit = vi.fn();
    const user = userEvent.setup({ delay: null });
    const selector = (value: (typeof values)[number]) => (
      <SteppedSelector
        activeTone={() => "primary"}
        disabled={false}
        value={value}
        values={values}
        onCommit={onCommit}
      >
        {(preview) => <output>{preview}</output>}
      </SteppedSelector>
    );
    const { rerender } = render(selector("low"));
    const slider = screen.getByRole("slider");
    capturePointer(slider);
    await user.pointer({ target: slider, keys: "[MouseLeft>]", coords: { x: 0, y: 12 } });
    await user.pointer({ target: slider, coords: { x: 200, y: 12 } });
    rerender(selector("high"));
    await user.pointer({ target: slider, keys: "[/MouseLeft]" });
    expect(onCommit).not.toHaveBeenCalled();
  } finally {
    bounds.mockRestore();
  }
});
