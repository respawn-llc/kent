import { createRoot } from "react-dom/client";

import { DevShowcaseApp } from "./DevShowcase";
import "../styles.css";

const root = document.getElementById("root");

if (root === null) {
  throw new Error("Missing #root element");
}

createRoot(root).render(<DevShowcaseApp />);
