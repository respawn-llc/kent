import { useCallback, useEffect, useMemo, useState, type ReactElement } from "react";

import {
  AppServicesProvider,
  TranscriptWindow,
  type TranscriptRenderSlots,
  type TranscriptWindowInput,
  type TranscriptPageRequest,
} from "@/app-facade";
import { TranscriptReasoningSlot, TranscriptThinkingStatus } from "@/features/chat";
import { TranscriptWindowView, type TranscriptViewportMeasurement } from "@/shared/transcript-window";
import { Toaster, TooltipProvider } from "@/ui";

import {
  fixtureHydration,
  fixtureIdle,
  fixtureKindControls,
  fixturePage,
  fixtureReasoningRow,
  fixtureRunning,
  fixtureStatus,
  fixtureTrace,
  type FixtureActivity,
} from "./thinkingFixtures";
import { replayDurationMs, replayText } from "./thinkingReplay";
import { thinkingShowcaseServices } from "./thinkingShowcaseServices";

const slots: TranscriptRenderSlots<ReactElement | null> = {
  user: (item) => <p>{item.value.Text}</p>,
  assistant: (item) => <p>{item.value.Text}</p>,
  tool: (item) => <p>{item.value.ToolName}</p>,
  reasoning: (item) => <TranscriptReasoningSlot item={item} />,
  notice: () => null,
  thinkingStatus: (presentation) => <TranscriptThinkingStatus presentation={presentation} />,
};
const estimateSize = () => 60;

export function ThinkingShowcase() {
  const [owner] = useState(() => {
    const window = new TranscriptWindow();
    window.dispatch({ kind: "initial-hydration", hydration: fixtureHydration() });
    return window;
  });
  const [snapshot, setSnapshot] = useState(owner.snapshot);
  const [activity, setActivity] = useState(fixtureRunning);
  const [streaming, setStreaming] = useState(false);
  const [clipboardFails, setClipboardFails] = useState(false);
  const [clipboardResult, setClipboardResult] = useState<string | null>(null);
  const [pageFails, setPageFails] = useState(false);
  const [pendingPage, setPendingPage] = useState<TranscriptPageRequest | null>(null);
  const [measurementDisplay, setMeasurementDisplay] = useState<TranscriptViewportMeasurement | null>(null);
  const [position, setPosition] = useState(180);
  const [sequence, setSequence] = useState(121);
  const dispatch = useCallback(
    (input: TranscriptWindowInput) => {
      const result = owner.dispatch(input);
      if (result.kind === "contract-failure") throw result.error;
      for (const effect of result.effects) {
        if (effect.kind === "page-request") {
          setPendingPage(effect.request);
        }
      }
      setSnapshot(owner.snapshot);
    },
    [owner],
  );
  const services = useMemo(
    () =>
      thinkingShowcaseServices(async (text) => {
        if (clipboardFails) throw new Error("Fixture clipboard failure.");
        await navigator.clipboard.writeText(text);
        setClipboardResult(text);
      }),
    [clipboardFails],
  );
  const sendActivity = (next: FixtureActivity) => {
    setActivity(next);
    dispatch({ kind: "runtime-activity", activity: next });
  };
  const advance = useCallback(() => {
    const next = Math.min(replayText.length, position + 16);
    setPosition(next);
    dispatch({ kind: "live-fact", fact: { kind: "reasoning_trace_update", payload: fixtureTrace(next) } });
    if (next === replayText.length) setStreaming(false);
  }, [dispatch, position]);
  useEffect(() => {
    if (!streaming) return;
    const timer = window.setInterval(advance, 100);
    return () => {
      window.clearInterval(timer);
    };
  }, [advance, streaming]);

  const reset = () => {
    setStreaming(false);
    setPosition(180);
    setSequence(121);
    setPendingPage(null);
    setActivity(fixtureRunning);
    dispatch({ kind: "initial-hydration", hydration: fixtureHydration() });
  };
  const commit = () => {
    setStreaming(false);
    const trace = snapshot.items.find((item) => item.kind === "reasoning_trace" && item.state === "live");
    if (trace === undefined) return;
    dispatch({ kind: "committed-row", row: fixtureReasoningRow(sequence, replayDurationMs, trace.value) });
    setSequence(sequence + 1);
  };
  const actions: readonly Readonly<{ label: string; run: () => void }>[] = [
    { label: "Reset live replay", run: reset },
    {
      label: streaming ? "Pause replay" : "Stream replay (simulated)",
      run: () => {
        setStreaming(!streaming);
      },
    },
    { label: "Advance 16 characters", run: advance },
    { label: "Commit first live trace", run: commit },
    {
      label: "Finish main work",
      run: () => {
        setStreaming(false);
        sendActivity({ ...fixtureIdle, Reviewer: activity.Reviewer });
      },
    },
    {
      label: "Reviewer invokes",
      run: () => {
        sendActivity({ ...activity, Reviewer: "invoking" });
      },
    },
    {
      label: "Reviewer finishes",
      run: () => {
        sendActivity({ ...activity, Reviewer: "inactive" });
      },
    },
    {
      label: "Question / Approval wait",
      run: () => {
        sendActivity({ ...activity, State: "awaiting_prompt" });
      },
    },
    {
      label: "Resume main work",
      run: () => {
        sendActivity({ ...fixtureRunning, Reviewer: activity.Reviewer });
      },
    },
    {
      label: "Supply new status",
      run: () => {
        dispatch(fixtureStatus("Inspecting the next part of this implementation."));
      },
    },
    {
      label: "Supply wrapped status",
      run: () => {
        dispatch(
          fixtureStatus(
            "Inspecting the full implementation and its dependencies while continuing the current work. This long fixture verifies two-line wrapping at narrow widths without shifting the first-line spinner alignment.",
          ),
        );
      },
    },
    {
      label: "Provider retry reset",
      run: () => {
        setStreaming(false);
        setPosition(0);
        dispatch({
          kind: "live-fact",
          fact: { kind: "reasoning_trace_reset", payload: { StepID: "fixture-step" } },
        });
      },
    },
    {
      label: "Assistant output",
      run: () => {
        dispatch({
          kind: "live-fact",
          fact: {
            kind: "assistant_delta",
            payload: {
              StepID: "fixture-step",
              StreamID: "fixture-assistant",
              Phase: "commentary",
              Delta: "Streaming assistant content. ",
            },
          },
        });
      },
    },
    {
      label: "Tool starts",
      run: () => {
        dispatch({
          kind: "live-fact",
          fact: {
            kind: "tool_start",
            payload: {
              StepID: "fixture-step",
              ToolCallID: "fixture-tool",
              ToolName: "shell",
              Presentation: null,
            },
          },
        });
      },
    },
    {
      label: "Add second trace",
      run: () => {
        dispatch({
          kind: "live-fact",
          fact: {
            kind: "reasoning_trace_update",
            payload: fixtureTrace(replayText.length, "second-trace"),
          },
        });
      },
    },
    {
      label: "Hydration/live handoff",
      run: () => {
        dispatch({
          kind: "reattachment-hydration",
          hydration: fixtureHydration(activity, [
            fixtureTrace(position),
            fixtureTrace(replayText.length, "second-trace"),
          ]),
        });
      },
    },
    {
      label: "Committed duration catalog",
      run: () => {
        setStreaming(false);
        setActivity(fixtureIdle);
        dispatch({
          kind: "initial-hydration",
          hydration: fixtureHydration(
            fixtureIdle,
            [],
            [replayDurationMs, 0, 3600123, null].map((duration, index) =>
              fixtureReasoningRow(
                index + 1,
                duration,
                fixtureTrace(replayText.length, `duration-${String(index)}`),
              ),
            ),
          ),
        });
      },
    },
    {
      label: "Load bounded scrolling history",
      run: () => {
        reset();
        const latest = fixturePage(false);
        dispatch({
          kind: "initial-hydration",
          hydration: {
            ...fixtureHydration(fixtureRunning, [fixtureTrace(180)], latest.entries),
            TailSegment: { Entries: latest.entries, OlderCursor: 1, HasMoreAbove: true },
          },
        });
      },
    },
    {
      label: "Lose observation",
      run: () => {
        setStreaming(false);
        dispatch({ kind: "observation-loss" });
      },
    },
  ];
  return (
    <AppServicesProvider services={services}>
      <TooltipProvider>
        <section className="grid min-w-0 gap-[var(--space-3)]">
          <h2 className="text-lg font-semibold">Thinking surfaces — production components</h2>
          <p className="text-sm text-[var(--color-muted)]">
            Full local-model reasoning, Session 4a0c7638-0956-475c-924c-9bdf58d97ea3, local_entry 7, recorded
            duration 12805ms. Arrival cadence and other fixtures are simulated. No Kent server or model is
            used.
          </p>
          <div className="flex flex-wrap gap-[var(--space-2)]">
            {fixtureKindControls.map((kind) => (
              <button
                className="rounded-[var(--radius-s)] bg-[var(--color-island-2)] p-[var(--space-2)] text-xs"
                key={kind}
                onClick={() => {
                  sendActivity({
                    ...fixtureRunning,
                    Reviewer: activity.Reviewer,
                    ActiveStep: { RunID: "fixture-run", StepID: "fixture-step", ActiveKind: kind },
                  });
                }}
                type="button"
              >
                {kind}
              </button>
            ))}
            {actions.map(({ label, run }) => (
              <button
                className="rounded-[var(--radius-s)] bg-[var(--color-island-2)] p-[var(--space-2)] text-xs"
                key={label}
                onClick={run}
                type="button"
              >
                {label}
              </button>
            ))}
          </div>
          <label>
            <input
              type="checkbox"
              checked={clipboardFails}
              onChange={(event) => {
                setClipboardFails(event.currentTarget.checked);
              }}
            />{" "}
            Force clipboard failure
          </label>
          <label>
            <input
              type="checkbox"
              checked={pageFails}
              onChange={(event) => {
                setPageFails(event.currentTarget.checked);
              }}
            />{" "}
            Force history-read failure (then scroll to a boundary)
          </label>
          {pendingPage === null ? null : (
            <button
              type="button"
              onClick={() => {
                dispatch(
                  pageFails
                    ? {
                        kind: "page-failure",
                        request: pendingPage,
                        error: new Error("Fixture history read failed."),
                      }
                    : {
                        kind: "page-success",
                        request: pendingPage,
                        page: fixturePage(pendingPage.direction === "older"),
                      },
                );
                setPendingPage(null);
              }}
            >
              Complete pending history read
            </button>
          )}
          <div className="h-[480px] min-w-0 overflow-hidden rounded-[var(--radius-m)] border border-[var(--color-outline)]">
            <TranscriptWindowView
              snapshot={snapshot}
              slots={slots}
              estimateSize={estimateSize}
              loadingLabel="Loading fixture history"
              retryLabel="Retry fixture history"
              boundaryErrorMessage={(error) => error.message}
              onInput={dispatch}
              onMeasurement={setMeasurementDisplay}
            />
          </div>
          <p className="text-sm text-[var(--color-muted)]">
            Composer position marker — status must scroll inside the transcript above, not stay here.
          </p>
          {clipboardResult === null ? null : (
            <pre className="whitespace-pre-wrap text-xs">Clipboard source: {clipboardResult}</pre>
          )}
          {measurementDisplay === null ? null : (
            <pre className="whitespace-pre-wrap text-xs">{JSON.stringify(measurementDisplay, null, 2)}</pre>
          )}
        </section>
        <Toaster />
      </TooltipProvider>
    </AppServicesProvider>
  );
}
