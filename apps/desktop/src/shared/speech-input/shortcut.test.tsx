import { afterEach, expect, it, vi } from "bun:test";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { installTestMicrophone } from "@/test-support/microphone";
import { getI18n } from "react-i18next";
import { SpeechDestinationProvider, useSpeechField } from "./useSpeechField";

let microphone: ReturnType<typeof installTestMicrophone> | undefined;

afterEach(() => {
  microphone?.restore();
  vi.restoreAllMocks();
});

function Field({ label, readOnly = false }: Readonly<{ label: string; readOnly?: boolean }>) {
  const [value, onChange] = useState("");
  const { editorRef, control } = useSpeechField({ value, onChange, readOnly });
  return (
    <>
      <textarea
        aria-label={label}
        ref={editorRef}
        value={value}
        readOnly={readOnly}
        onChange={(event) => {
          onChange(event.target.value);
        }}
      />
      {control}
    </>
  );
}

async function setup() {
  microphone = installTestMicrophone();
  const services = createTestServices([]);
  vi.spyOn(services.api, "speechAvailability").mockResolvedValue({
    uploadUrl: "/speech",
    encodings: [{ mimeType: "audio/webm", extension: "webm" }],
    maxBytes: 25_000_000,
    maxDurationSeconds: 600,
  });
  vi.spyOn(services.speech, "begin").mockResolvedValue({
    append: async () => ({ bytes: 2000, limitReached: false }),
    finalize: async () => ({
      kind: "native",
      original: { path: "/temporary/voice.webm", mimeType: "audio/webm", bytes: 2000 },
      durationMs: 2000,
    }),
    result: async () => ({ kind: "transcript", text: "dictated" }),
    cancel: async () => undefined,
  });
  vi.spyOn(services.speech, "discard").mockResolvedValue();
  render(
    <TestAppProviders services={services} skipAnimations>
      <SpeechDestinationProvider>
        <Field label="first" />
        <Field label="second" />
        <Field label="readonly" readOnly />
        <input aria-label="unsupported" />
      </SpeechDestinationProvider>
    </TestAppProviders>,
  );
  await screen.findAllByRole("button", { name: getI18n().t("speech.record") });
}

it.each(["altKey", "ctrlKey"] as const)(
  "%s+Space starts dictation only in the focused field, ahead of global search",
  async (modifier) => {
    await setup();
    const search = vi.fn();
    window.addEventListener("keydown", search);
    try {
      const field = screen.getByRole("textbox", { name: "second" });
      field.focus();
      expect(fireEvent.keyDown(field, { code: "Space", key: " ", [modifier]: true })).toBe(false);
      await screen.findByRole("button", { name: getI18n().t("speech.stop") });
      expect(microphone?.startRequested).toHaveBeenCalledTimes(1);
      expect(search).not.toHaveBeenCalled();
      expect(screen.getByRole("button", { name: getI18n().t("speech.record") })).toBeDisabled();
      fireEvent.keyDown(field, { code: "Space", key: " ", [modifier]: true, repeat: true });
      expect(microphone?.startRequested).toHaveBeenCalledTimes(1);
      fireEvent.click(screen.getByRole("button", { name: getI18n().t("speech.stop") }));
      await waitFor(() => {
        expect(field).toHaveValue("dictated");
      });
      expect(screen.getByRole("textbox", { name: "first" })).toHaveValue("");
    } finally {
      window.removeEventListener("keydown", search);
    }
  },
);

it.each([
  { code: "Space", key: " " },
  { code: "Space", key: " ", altKey: true, ctrlKey: true },
  { code: "Space", key: " ", altKey: true, shiftKey: true },
  { code: "Space", key: " ", ctrlKey: true, metaKey: true },
  { code: "Space", key: " ", altKey: true, isComposing: true },
  { code: "Space", key: " ", ctrlKey: true, repeat: true },
  { code: "KeyS", key: "s", ctrlKey: true },
])("does not start recording for unsupported or repeated keys: %j", async (keys) => {
  await setup();
  const field = screen.getByRole("textbox", { name: "first" });
  field.focus();
  fireEvent.keyDown(field, keys);
  expect(microphone?.startRequested).not.toHaveBeenCalled();
});

it.each(["readonly", "unsupported"])("leaves shortcuts in %s fields alone", async (name) => {
  await setup();
  const field = screen.getByRole("textbox", { name });
  field.focus();
  expect(fireEvent.keyDown(field, { code: "Space", key: " ", altKey: true })).toBe(true);
  expect(microphone?.startRequested).not.toHaveBeenCalled();
});
