import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { App } from "./App";
import { createDefaultAppServices } from "./appEnvironment";
import { createDevelopmentReactErrorHandlers } from "../reactDiagnostics";
import "./styles.css";

const root = document.getElementById("root");

if (root === null) {
  throw new Error("Missing #root element");
}

const services = await createDefaultAppServices();

createRoot(root, import.meta.env.DEV ? createDevelopmentReactErrorHandlers(services.logger) : {}).render(
  <StrictMode>
    <App services={services} />
  </StrictMode>,
);
