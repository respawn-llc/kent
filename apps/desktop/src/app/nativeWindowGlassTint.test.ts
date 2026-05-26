import { parseCssColorToNativeTint } from "./nativeWindowGlassTint";

describe("parseCssColorToNativeTint", () => {
  it("parses rgba colors into AppKit unit channels", () => {
    expect(parseCssColorToNativeTint("rgba(24, 22, 20, 0.82)")).toEqual({
      alpha: 0.82,
      blue: 20 / 255,
      green: 22 / 255,
      red: 24 / 255,
    });
  });

  it("parses modern rgb colors with slash alpha", () => {
    expect(parseCssColorToNativeTint("rgb(255 255 255 / 46%)")).toEqual({
      alpha: 0.46,
      blue: 1,
      green: 1,
      red: 1,
    });
  });
});
