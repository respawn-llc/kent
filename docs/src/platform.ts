export type OperatingSystem = "windows" | "macos" | "linux";

interface PlatformSource {
  platform?: string;
  userAgentData?: { platform?: string };
}

const platforms = new Map<string, OperatingSystem>([
  ["Windows", "windows"],
  ["Win32", "windows"],
  ["Win64", "windows"],
  ["macOS", "macos"],
  ["MacIntel", "macos"],
  ["MacARM", "macos"],
  ["MacPPC", "macos"],
  ["Linux", "linux"],
  ["Linux x86_64", "linux"],
  ["Linux i686", "linux"],
  ["Linux armv7l", "linux"],
  ["Linux armv8l", "linux"],
  ["Linux aarch64", "linux"],
  ["X11", "linux"],
]);

const tabLabels = {
  windows: "Windows",
  macos: "macOS",
  linux: "Linux",
} as const;

export function detectOperatingSystem(
  source: PlatformSource,
): OperatingSystem | undefined {
  for (const platform of [source.userAgentData?.platform, source.platform]) {
    if (platform === undefined) continue;
    const operatingSystem = platforms.get(platform);
    if (operatingSystem !== undefined) return operatingSystem;
  }
  return undefined;
}

export function selectOperatingSystemTab(
  operatingSystem: OperatingSystem | undefined,
  storedPreference: string | null,
): (typeof tabLabels)[OperatingSystem] | undefined {
  for (const label of Object.values(tabLabels)) {
    if (storedPreference === label) return label;
  }
  return operatingSystem === undefined ? undefined : tabLabels[operatingSystem];
}
