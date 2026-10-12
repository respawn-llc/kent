import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nextProvider } from "react-i18next";

import { Popover } from "@/ui";
import { appI18n, initializeI18n } from "@/i18n";
import { installResizeObserverGeometry } from "@/test-support/resize-observer";
import { ChatSettingsChip } from "./ChatSettingsChip";

it("shows the model tooltip only while its text is truncated", async () => {
  await initializeI18n();
  const geometry = installResizeObserverGeometry();
  let width = 200;
  const clientWidth = vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockImplementation(() => width);
  const scrollWidth = vi.spyOn(HTMLElement.prototype, "scrollWidth", "get").mockReturnValue(150);
  const user = userEvent.setup();
  const view = render(
    <I18nextProvider i18n={appI18n}>
      <Popover>
        <ChatSettingsChip role="default" model="full-model-name" thinking="medium" fast={false} />
      </Popover>
    </I18nextProvider>,
  );
  try {
    const trigger = screen.getByRole("button");
    await user.hover(trigger);
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
    act(() => {
      width = 100;
      geometry.notify();
    });
    expect(await screen.findByRole("tooltip")).toHaveTextContent("full-model-name");
    act(() => {
      width = 200;
      geometry.notify();
    });
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  } finally {
    view.unmount();
    clientWidth.mockRestore();
    scrollWidth.mockRestore();
    geometry.restore();
  }
});
