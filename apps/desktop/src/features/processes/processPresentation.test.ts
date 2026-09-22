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
  backgrounded: true,
  killRequested: true,
};

it("terminal process facts end stopping", () => {
  const presentation = projectProcessPresentation(killedProcess, 3000);
  expect(presentation.stopping).toBe(false);
  expect(presentation.stateIndicator).toBe("terminal");
  expect(presentation.terminable).toBe(false);
});

it.each([false, true])("running process stopping follows server stop-request state (%s)", (killRequested) => {
  const presentation = projectProcessPresentation(
    { ...killedProcess, state: "running", running: true, finishedAt: null, exitCode: null, killRequested },
    3000,
  );
  expect(presentation.stopping).toBe(killRequested);
  expect(presentation.terminable).toBe(!killRequested);
  expect(presentation.stateIndicator).toBe(killRequested ? "stopping" : "active");
});
