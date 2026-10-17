import { useEffect, useRef, useState, type RefObject } from "react";
import { createPortal } from "react-dom";
import { Mic, Square } from "lucide-react";
import { useReducedMotion } from "motion/react";
import { IconTooltipButton } from "./IconTooltipButton";

export type RecordingControlProps = Readonly<{
  phase: "idle" | "preparing" | "recording" | "transcribing";
  recording: Readonly<{ analyser: AnalyserNode; startedAt: number }> | null;
  disabled: boolean;
  label: string;
  tooltip: string;
  onStart(): void;
  onStop(): void;
}>;

export function RecordingControl({
  phase,
  recording,
  disabled,
  label,
  tooltip,
  onStart,
  onStop,
}: RecordingControlProps) {
  const anchor = useRef<HTMLSpanElement>(null);
  return (
    <span className="relative inline-flex shrink-0 items-center gap-[var(--space-2)]">
      {recording !== null && <RecordingElapsed startedAt={recording.startedAt} />}
      <span className={recording === null ? "inline-flex" : "inline-flex px-[var(--space-4)]"}>
        <span ref={anchor} className="relative inline-grid place-items-center text-[var(--color-primary)]">
          {recording !== null && <RadialRecordingMeter analyser={recording.analyser} anchor={anchor} />}
          <IconTooltipButton
            label={label}
            tooltip={tooltip}
            disabled={disabled}
            loading={phase === "preparing" || phase === "transcribing"}
            onClick={phase === "recording" ? onStop : onStart}
          >
            {phase === "recording" ? <Square size={16} fill="currentColor" /> : <Mic size={18} />}
          </IconTooltipButton>
        </span>
      </span>
    </span>
  );
}

function RecordingElapsed({ startedAt }: Readonly<{ startedAt: number }>) {
  const [seconds, setSeconds] = useState(0);
  useEffect(() => {
    const update = () => {
      setSeconds(Math.floor((performance.now() - startedAt) / 1000));
    };
    update();
    const timer = setInterval(update, 250);
    return () => {
      clearInterval(timer);
    };
  }, [startedAt]);
  return (
    <span className="font-mono text-xs tabular-nums text-[var(--color-muted)]">
      {Math.floor(seconds / 60)
        .toString()
        .padStart(2, "0")}
      :{(seconds % 60).toString().padStart(2, "0")}
    </span>
  );
}

/*
Adapted from ElevenLabs UI Live Waveform.
MIT License. Copyright (c) 2025 Eleven Labs Inc.
Permission is hereby granted, free of charge, to any person obtaining a copy of
this software and associated documentation files (the "Software"), to deal in
the Software without restriction, including without limitation the rights to
use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of
the Software, and to permit persons to whom the Software is furnished to do so,
subject to the following conditions:
The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.
THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/
function RadialRecordingMeter({
  analyser,
  anchor,
}: Readonly<{ analyser: AnalyserNode; anchor: RefObject<HTMLSpanElement | null> }>) {
  const canvas = useRef<HTMLCanvasElement>(null);
  const reducedMotion = useReducedMotion();
  useEffect(() => {
    const element = canvas.current;
    const context = element?.getContext("2d");
    if (element == null || context == null) return;
    const data = new Uint8Array(analyser.frequencyBinCount);
    analyser.getByteFrequencyData(data);
    let frame: number | undefined;
    const draw = () => {
      const button = anchor.current;
      const container = button?.parentElement;
      if (button == null || container == null) return;
      const bounds = button.getBoundingClientRect();
      element.style.left = `${String(bounds.left + bounds.width / 2)}px`;
      element.style.top = `${String(bounds.top + bounds.height / 2)}px`;
      const padding = Number.parseFloat(getComputedStyle(container).paddingLeft);
      element.style.width = `${String(bounds.width + padding * 2)}px`;
      const size = element.getBoundingClientRect().width;
      const scale = window.devicePixelRatio;
      if (element.width !== Math.round(size * scale)) {
        element.width = Math.round(size * scale);
        element.height = element.width;
      }
      context.setTransform(scale, 0, 0, scale, 0, 0);
      context.clearRect(0, 0, size, size);
      context.strokeStyle = getComputedStyle(element).color;
      context.lineWidth = size / 48;
      context.lineCap = "round";
      if (!reducedMotion) analyser.getByteFrequencyData(data);
      context.beginPath();
      const bars = data.subarray(0, 56);
      const count = bars.length;
      const radius = bounds.width / 2 + 1;
      for (const [index, value] of bars.entries()) {
        const angle = (index / count) * Math.PI * 2 - Math.PI / 2;
        const amplitude = (value / 255) * padding * 0.75;
        const outer = radius + size / 48 + amplitude;
        context.moveTo(size / 2 + Math.cos(angle) * radius, size / 2 + Math.sin(angle) * radius);
        context.lineTo(size / 2 + Math.cos(angle) * outer, size / 2 + Math.sin(angle) * outer);
      }
      context.stroke();
      frame = requestAnimationFrame(draw);
    };
    draw();
    return () => {
      if (frame !== undefined) cancelAnimationFrame(frame);
    };
  }, [analyser, reducedMotion, anchor]);
  return createPortal(
    <canvas
      ref={canvas}
      className="pointer-events-none fixed z-[60] aspect-square -translate-x-1/2 -translate-y-1/2 text-[var(--color-primary)]"
    />,
    document.body,
  );
}
