import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { vi } from "vitest";

import { StatusProvider } from "./statusStore";
import { useNativeDialogFallback } from "./useNativeDialogFallback";

describe("useNativeDialogFallback", () => {
  it("shows one toast and fallback when native open fails", async () => {
    const openNative = vi.fn<() => Promise<void>>().mockRejectedValue(new Error("denied"));

    render(<Harness openNative={openNative} />);

    fireEvent.click(screen.getByRole("button", { name: "Open" }));

    expect(await screen.findByRole("dialog")).toHaveTextContent("first");
    expect(screen.getByText("Native dialog failed")).toBeInTheDocument();
    expect(screen.getByText("denied")).toBeInTheDocument();
    expect(screen.getAllByText("Native dialog failed")).toHaveLength(1);
  });

  it("opens fallback without a toast when native dialogs are unavailable", async () => {
    const openNative = vi.fn<() => Promise<void>>();

    render(<Harness nativeAvailable={false} openNative={openNative} />);

    fireEvent.click(screen.getByRole("button", { name: "Open" }));

    expect(await screen.findByRole("dialog")).toHaveTextContent("first");
    expect(screen.queryByText("Native dialog failed")).not.toBeInTheDocument();
    expect(openNative).not.toHaveBeenCalled();
  });

  it("clears fallback when native retry succeeds", async () => {
    const openNative = vi
      .fn<() => Promise<void>>()
      .mockRejectedValueOnce(new Error("denied"))
      .mockResolvedValueOnce();

    render(<Harness openNative={openNative} />);

    fireEvent.click(screen.getByRole("button", { name: "Open" }));
    expect(await screen.findByRole("dialog")).toHaveTextContent("first");

    fireEvent.click(screen.getByRole("button", { name: "Open" }));

    await waitFor(() => {
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });
  });

  it("replaces fallback payload on failed retry", async () => {
    const openNative = vi.fn<() => Promise<void>>().mockRejectedValue(new Error("denied"));

    render(<Harness openNative={openNative} />);

    fireEvent.click(screen.getByRole("button", { name: "Open" }));
    expect(await screen.findByRole("dialog")).toHaveTextContent("first");

    fireEvent.click(screen.getByRole("button", { name: "Use second payload" }));
    fireEvent.click(screen.getByRole("button", { name: "Open" }));

    expect(await screen.findByRole("dialog")).toHaveTextContent("second");
    expect(screen.getAllByText("Native dialog failed")).toHaveLength(1);
  });
});

function Harness({
  nativeAvailable,
  openNative,
}: Readonly<{ nativeAvailable?: boolean | undefined; openNative: () => Promise<void> }>) {
  return (
    <StatusProvider>
      <HarnessContent nativeAvailable={nativeAvailable} openNative={openNative} />
    </StatusProvider>
  );
}

function HarnessContent({
  nativeAvailable,
  openNative,
}: Readonly<{ nativeAvailable?: boolean | undefined; openNative: () => Promise<void> }>) {
  const [payload, setPayload] = useState("first");
  const dialog = useNativeDialogFallback<string>({
    errorNoticeID: "native-dialog-error",
    errorTitle: "Native dialog failed",
    nativeAvailable,
    openNative,
    renderFallback: (value) => <div role="dialog">{value}</div>,
  });

  return (
    <>
      <button onClick={() => void dialog.open(payload)} type="button">
        Open
      </button>
      <button
        onClick={() => {
          setPayload("second");
        }}
        type="button"
      >
        Use second payload
      </button>
      {dialog.fallback}
    </>
  );
}
