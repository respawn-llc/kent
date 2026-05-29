import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { vi } from "vitest";

import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger } from "./index";

describe("ContextMenu", () => {
  it("renders island-styled menu items through a portal and dismisses with Escape", async () => {
    const onSelect = vi.fn();
    render(
      <ContextMenu>
        <ContextMenuTrigger asChild>
          <button type="button">Open actions</button>
        </ContextMenuTrigger>
        <ContextMenuContent>
          <ContextMenuItem onSelect={onSelect}>Create node group</ContextMenuItem>
        </ContextMenuContent>
      </ContextMenu>,
    );

    fireEvent.contextMenu(screen.getByRole("button", { name: "Open actions" }));

    const menu = await screen.findByRole("menu");
    expect(menu).toHaveClass("island-surface", "island-surface-2", "app-region-no-drag");
    expect(document.body).toContainElement(menu);
    expect(screen.getByRole("menuitem", { name: "Create node group" })).toHaveClass(
      "data-[highlighted]:bg-[var(--color-island-2)]",
    );

    fireEvent.keyDown(menu, { key: "Escape" });

    await waitFor(() => {
      expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    });
  });
});
