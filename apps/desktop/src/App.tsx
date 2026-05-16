import { nativeBridge } from "./appEnvironment";

export function App() {
  return (
    <main className="shell">
      <section className="hero" aria-labelledby="app-title">
        <p className="eyebrow">Builder GUI foundation</p>
        <h1 id="app-title">Builder Desktop</h1>
        <p className="summary">
          Tauri, React, and TypeScript shell for the async workflow client. Server remains source of truth;
          desktop stays a remote-control surface.
        </p>
        <dl className="facts" aria-label="Native capability status">
          <div>
            <dt>Clipboard</dt>
            <dd>{nativeBridge.capabilities.clipboard.writeText ? "available" : "unavailable"}</dd>
          </div>
          <div>
            <dt>Notifications</dt>
            <dd>{nativeBridge.capabilities.notifications.basic ? "available" : "unavailable"}</dd>
          </div>
        </dl>
      </section>
    </main>
  );
}
