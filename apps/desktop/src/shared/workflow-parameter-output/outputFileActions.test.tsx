import { render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "bun:test";
import { Toaster } from "sonner";
import { createInstance } from "i18next";

import { createTestServices } from "@/test-support/app-services";
import { openOutputFile } from "./outputFileActions";

const translations = createInstance();
await translations.init({ lng: "en", resources: {} });

it("copies without native requests when native access or the managed execution root is unavailable", async () => {
  const services = createTestServices([]);
  const open = vi.fn(async () => undefined);
  const write = vi.fn(async () => undefined);
  const native = {
    ...services.nativeBridge,
    capabilities: { ...services.nativeBridge.capabilities, clipboard: { writeText: true, readText: false } },
    clipboard: { ...services.nativeBridge.clipboard, writeText: write },
    files: { ...services.nativeBridge.files, openDefault: open },
  };
  await openOutputFile("../a%20#?.txt", { kind: "unavailable" }, native, translations.t);
  expect(open).not.toHaveBeenCalled();
  expect(write).toHaveBeenCalledWith("../a%20#?.txt");
});

it("keeps an attempted open failure visible after a successful path copy", async () => {
  const services = createTestServices([]);
  const failure = new Error("OS denied opening this fixture");
  const write = vi.fn(async () => undefined);
  const native = {
    ...services.nativeBridge,
    capabilities: {
      ...services.nativeBridge.capabilities,
      files: { ...services.nativeBridge.capabilities.files, open: true },
      clipboard: { writeText: true, readText: false },
    },
    clipboard: { ...services.nativeBridge.clipboard, writeText: write },
    files: {
      ...services.nativeBridge.files,
      openDefault: vi.fn(async () => {
        throw failure;
      }),
    },
  };
  render(<Toaster />);
  await openOutputFile("/tmp/file.txt", { kind: "unavailable" }, native, translations.t);
  expect(write).toHaveBeenCalledWith("/tmp/file.txt");
  expect(await screen.findByText(failure.message)).toBeInTheDocument();
});

it("reports both diagnostics when opening and copying fail", async () => {
  const services = createTestServices([]);
  const openFailure = new Error("fixture OS open failure");
  const copyFailure = new Error("fixture clipboard failure");
  const native = {
    ...services.nativeBridge,
    capabilities: {
      ...services.nativeBridge.capabilities,
      files: { ...services.nativeBridge.capabilities.files, open: true },
      clipboard: { writeText: true, readText: false },
    },
    clipboard: {
      ...services.nativeBridge.clipboard,
      writeText: vi.fn(async () => {
        throw copyFailure;
      }),
    },
    files: {
      ...services.nativeBridge.files,
      openDefault: vi.fn(async () => {
        throw openFailure;
      }),
    },
  };
  render(<Toaster />);
  await openOutputFile("/tmp/file.txt", { kind: "unavailable" }, native, translations.t);
  await waitFor(() => {
    expect(screen.getByText(openFailure.message)).toBeInTheDocument();
    expect(screen.getByText(copyFailure.message)).toBeInTheDocument();
  });
});
