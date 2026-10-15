import { expect, it } from "bun:test";
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
  const clientWidth = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "clientWidth");
  const scrollWidth = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "scrollWidth");
  Object.defineProperty(HTMLElement.prototype, "clientWidth", { configurable: true, get: () => width });
  Object.defineProperty(HTMLElement.prototype, "scrollWidth", { configurable: true, get: () => 150 });
  const user = userEvent.setup();
  const view = render(
    <I18nextProvider i18n={appI18n}>
      <Popover>
        <ChatSettingsChip
          role="default"
          model="full-model-name"
          thinking="medium"
          fast={false}
          triggerProps={{}}
        />
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
    if (clientWidth) Object.defineProperty(HTMLElement.prototype, "clientWidth", clientWidth);
    else Reflect.deleteProperty(HTMLElement.prototype, "clientWidth");
    if (scrollWidth) Object.defineProperty(HTMLElement.prototype, "scrollWidth", scrollWidth);
    else Reflect.deleteProperty(HTMLElement.prototype, "scrollWidth");
    geometry.restore();
  }
});
