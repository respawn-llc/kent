import {
  appChromeInlineTitleClassNames,
  appChromeTitleClassNames,
  appChromeTitlePlacementClassNames,
} from "./appChromeStyles";

export function WindowChromeTitle({
  title,
  inline = false,
  macOS,
}: Readonly<{ title: string; inline?: boolean; macOS: boolean }>) {
  return (
    <div
      className={
        inline
          ? appChromeInlineTitleClassNames.join(" ")
          : [...appChromeTitleClassNames, ...appChromeTitlePlacementClassNames(macOS)].join(" ")
      }
      data-testid="app-chrome-title"
    >
      {title}
    </div>
  );
}
