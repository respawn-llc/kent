import {
  appChromeInlineTitleClassNames,
  appChromeTitleClassNames,
  appChromeTitlePlacementClassNames,
} from "./appChromeStyles";

export function WindowChromeTitle({
  title,
  inline = false,
  macOS,
  contentWindow = false,
}: Readonly<{ title: string; inline?: boolean; macOS: boolean; contentWindow?: boolean }>) {
  return (
    <div
      className={
        inline
          ? appChromeInlineTitleClassNames.join(" ")
          : [
              ...appChromeTitleClassNames,
              ...(contentWindow
                ? ["left-[var(--native-home-link-left-macos)]", "text-left"]
                : appChromeTitlePlacementClassNames(macOS)),
            ].join(" ")
      }
      data-testid="app-chrome-title"
    >
      {title}
    </div>
  );
}
