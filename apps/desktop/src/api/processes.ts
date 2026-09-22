export type DesktopProcess = Readonly<{
  id: string;
  state: string;
  command: string;
  workdir: string;
  startedAt: number;
  finishedAt: number | null;
  exitCode: number | null;
  recentOutput: string;
  running: boolean;
  backgrounded: boolean;
  killRequested: boolean;
}>;

export class ProcessObservationError extends Error {
  readonly _tag = "ProcessObservationError";
}
