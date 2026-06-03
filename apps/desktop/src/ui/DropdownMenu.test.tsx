import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { CirclePlus } from "lucide-react";
import { vi } from "vitest";

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "./index";

describe("DropdownMenu", () => {
  it("renders content through a portal with icons and separators", async () => {
    const onSelect = vi.fn();
    render(
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button type="button">Open actions</button>
        </DropdownMenuTrigger>
        <DropdownMenuContent>
          <DropdownMenuItem onSelect={onSelect}>
            <CirclePlus aria-hidden="true" size={16} />
            Add node
          </DropdownMenuItem>
          <DropdownMenuSeparator data-testid="dropdown-menu-separator" />
          <DropdownMenuItem>Delete node</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>,
    );

    fireEvent.pointerDown(screen.getByRole("button", { name: "Open actions" }));

    const menu = await screen.findByRole("menu");
    expect(document.body).toContainElement(menu);
    expect(screen.getByRole("menuitem", { name: "Add node" })).toBeInTheDocument();
    expect(screen.getByTestId("dropdown-menu-separator")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("menuitem", { name: "Add node" }));
    expect(onSelect).toHaveBeenCalledOnce();

    await waitFor(() => {
      expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    });
  });
});
