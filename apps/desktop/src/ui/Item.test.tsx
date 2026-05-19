import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";

import { Item, ItemContent, ItemGroup, ItemTitle } from "./Item";

describe("Item", () => {
  it("renders a plain clickable item with shadcn-compatible slots", () => {
    const onClick = vi.fn();

    render(
      <ItemGroup data-testid="item-group">
        <Item onClick={onClick}>
          <ItemContent>
            <ItemTitle>Delivery</ItemTitle>
          </ItemContent>
        </Item>
      </ItemGroup>,
    );

    expect(screen.getByTestId("item-group")).toHaveAttribute("data-slot", "item-group");
    expect(screen.getByRole("button", { name: "Delivery" })).toHaveAttribute("data-slot", "item");
    expect(screen.getByText("Delivery")).toHaveAttribute("data-slot", "item-title");

    fireEvent.click(screen.getByRole("button", { name: "Delivery" }));

    expect(onClick).toHaveBeenCalledOnce();
  });

  it("keeps native button keyboard semantics and focus classes", async () => {
    const onClick = vi.fn();
    const user = userEvent.setup();

    render(<Item onClick={onClick}>Keyboard workflow</Item>);

    const item = screen.getByRole("button", { name: "Keyboard workflow" });
    item.focus();
    await user.keyboard("{Enter}");
    await user.keyboard(" ");

    expect(item).toHaveFocus();
    expect(item).toHaveAttribute("type", "button");
    expect(item).toHaveClass("focus-visible:border-[var(--color-primary)]");
    expect(onClick).toHaveBeenCalledTimes(2);
  });

  it("keeps disabled items inert", () => {
    const onClick = vi.fn();

    render(
      <Item disabled onClick={onClick}>
        Disabled workflow
      </Item>,
    );

    const item = screen.getByRole("button", { name: "Disabled workflow" });
    expect(item).toBeDisabled();

    fireEvent.click(item);

    expect(onClick).not.toHaveBeenCalled();
  });
});
