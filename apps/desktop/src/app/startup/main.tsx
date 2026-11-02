import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { App } from "./App";
import { createDefaultAppServices } from "./appEnvironment";
import { createDevelopmentReactErrorHandlers } from "../reactDiagnostics";
import { SettingsRecovery } from "./SettingsRecovery";
import "./styles.css";

const root = document.getElementById("root");

if (root === null) {
  throw new Error("Missing #root element");
}

const services = await createDefaultAppServices();

createRoot(
  root,
  import.meta.env.DEV && !("kind" in services) ? createDevelopmentReactErrorHandlers(services.logger) : {},
).render(
  <StrictMode>
    {"kind" in services ? <SettingsRecovery failure={services} /> : <App services={services} />}
  </StrictMode>,
);
