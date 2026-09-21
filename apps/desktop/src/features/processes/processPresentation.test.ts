import type { DesktopProcess } from "@/api";
import { projectProcessPresentation } from "./processPresentation";

const killedProcess: DesktopProcess = {
  id: "1000",
  state: "killed",
  command: "sleep 120 &",
  workdir: "/workspace",
  startedAt: 1000,
  finishedAt: 2000,
  exitCode: 0,
  recentOutput: "",
  running: false,
  killRequested: true,
};

it.each([false, true])(
  "terminal process facts end stopping even with pending request %s",
  (pendingTermination) => {
    const presentation = projectProcessPresentation(killedProcess, 3000, pendingTermination);
    expect(presentation.stopping).toBe(false);
    expect(presentation.stateIndicator).toBe("terminal");
    expect(presentation.terminable).toBe(false);
  },
);

it.each([
  [false, false, false],
  [true, false, true],
  [false, true, true],
  [true, true, true],
])(
  "running process stopping follows request facts (%s, %s)",
  (killRequested, pendingTermination, stopping) => {
    const presentation = projectProcessPresentation(
      { ...killedProcess, state: "running", running: true, finishedAt: null, exitCode: null, killRequested },
      3000,
      pendingTermination,
    );
    expect(presentation.stopping).toBe(stopping);
    expect(presentation.terminable).toBe(!stopping);
    expect(presentation.stateIndicator).toBe(stopping ? "stopping" : "active");
  },
);
