import { expect, it } from "bun:test";
import { browserSettingsFixture } from "@app/native-bridge";
import { effectiveAppearance, settingsRequireRestart } from "./desktopSettings";

it("applies launch low power without overwriting saved appearance choices", () => {
  const snapshot = {
    ...browserSettingsFixture,
    saved: { ...browserSettingsFixture.saved, transparency: false, reducedMotion: false },
    launch: { transparency: true, lowPower: true },
  };
  expect(effectiveAppearance(snapshot, false)).toEqual({
    reducedMotion: true,
    transparent: false,
    lowPower: true,
  });
  expect(snapshot.saved.reducedMotion).toBe(false);
  expect(settingsRequireRestart(snapshot)).toBe(true);
  expect(effectiveAppearance(browserSettingsFixture, true).reducedMotion).toBe(true);
  expect(effectiveAppearance(browserSettingsFixture, false).reducedMotion).toBe(false);
  expect(
    effectiveAppearance(
      {
        ...browserSettingsFixture,
        saved: { ...browserSettingsFixture.saved, reducedMotion: true },
      },
      false,
    ).reducedMotion,
  ).toBe(true);
  expect(
    effectiveAppearance(
      {
        ...browserSettingsFixture,
        launch: { transparency: false, lowPower: false },
      },
      false,
    ),
  ).toEqual({ reducedMotion: false, transparent: false, lowPower: false });
  expect(
    effectiveAppearance(
      {
        ...browserSettingsFixture,
        saved: { ...browserSettingsFixture.saved, lowPower: true },
      },
      false,
    ).lowPower,
  ).toBe(false);
  expect(settingsRequireRestart(browserSettingsFixture)).toBe(false);
  expect(
    settingsRequireRestart({
      ...browserSettingsFixture,
      saved: { ...browserSettingsFixture.saved, serverAddress: "wss://other.example/rpc" },
    }),
  ).toBe(true);
});
