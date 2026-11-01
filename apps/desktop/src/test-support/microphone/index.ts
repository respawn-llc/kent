import { vi } from "bun:test";

export function installTestMicrophone(manualStart = false) {
  const globals = new Map<string, PropertyDescriptor | undefined>();
  const stub = (name: string, value: unknown) => {
    globals.set(name, Object.getOwnPropertyDescriptor(globalThis, name));
    Object.defineProperty(globalThis, name, { configurable: true, writable: true, value });
  };
  const startRequested = vi.fn();
  const track = { stop: vi.fn() };
  let ready = () => undefined;
  stub("navigator", { mediaDevices: { getUserMedia: async () => ({ getTracks: () => [track] }) } });
  stub(
    "MediaRecorder",
    class extends EventTarget {
      static isTypeSupported() {
        return true;
      }
      state: RecordingState = "inactive";
      start() {
        this.state = "recording";
        startRequested();
        ready = () => {
          this.dispatchEvent(new Event("start"));
        };
        if (!manualStart) queueMicrotask(ready);
      }
      requestData() {
        return undefined;
      }
      stop() {
        this.state = "inactive";
        queueMicrotask(() => {
          this.dispatchEvent(new Event("stop"));
        });
      }
    },
  );
  stub(
    "AudioContext",
    class {
      async resume() {
        return undefined;
      }
      createAnalyser() {
        return { fftSize: 256, getByteTimeDomainData: (data: Uint8Array) => data.fill(140) };
      }
      createMediaStreamSource() {
        return {
          connect() {
            return undefined;
          },
        };
      }
      async close() {
        return undefined;
      }
    },
  );
  return {
    startRequested,
    track,
    ready: () => {
      ready();
    },
    restore() {
      for (const [name, descriptor] of globals) {
        if (descriptor === undefined) Reflect.deleteProperty(globalThis, name);
        else Object.defineProperty(globalThis, name, descriptor);
      }
    },
  };
}
