/// <reference types="node" />

import { readFileSync } from "node:fs";
import { join } from "node:path";

import { workflowGraphZOrder } from "./workflowGraphZOrder";

const workflowEditorCss = readFileSync(
  join(process.cwd(), "src/features/workflow-editor/workflow-editor.css"),
  "utf8",
);

describe("workflow editor CSS", () => {
  afterEach(() => {
    document.querySelectorAll("[data-workflow-editor-css-test]").forEach((element) => {
      element.remove();
    });
  });

  it("keeps edge paths above group islands and below node handles", () => {
    const declarations = cssDeclarationsBySelector(workflowEditorCss);

    expect(
      declarations.get(".workflow-editor-canvas .react-flow__node.workflow-graph-layer-group")?.get("z-index"),
    ).toBe(workflowGraphZOrder.group.toString());
    expect(declarations.get(".workflow-editor-canvas .react-flow__nodes")?.get("z-index")).toBe("auto");
    expect(declarations.get(".workflow-editor-canvas .react-flow__edges")?.get("z-index")).toBe(
      workflowGraphZOrder.edge.toString(),
    );
    expect(
      declarations.get(".workflow-editor-canvas .react-flow__edge.workflow-graph-layer-edge")?.get("z-index"),
    ).toBe(workflowGraphZOrder.edge.toString());
    expect(
      declarations.get(".workflow-editor-canvas .react-flow__edgelabel-renderer")?.get("z-index"),
    ).toBe(workflowGraphZOrder.edgeLabel.toString());
    expect(
      declarations.get(".workflow-editor-canvas .react-flow__node.workflow-graph-layer-node")?.get("z-index"),
    ).toBe(workflowGraphZOrder.node.toString());
  });
});

function cssDeclarationsBySelector(css: string): ReadonlyMap<string, ReadonlyMap<string, string>> {
  const style = document.createElement("style");
  style.dataset.workflowEditorCssTest = "true";
  style.textContent = css;
  document.head.append(style);
  const out = new Map<string, ReadonlyMap<string, string>>();
  for (const rule of Array.from(style.sheet?.cssRules ?? [])) {
    if (isStyleRule(rule)) {
      out.set(rule.selectorText, styleDeclarationMap(rule.style));
    }
  }
  return out;
}

function styleDeclarationMap(style: CSSStyleDeclaration): ReadonlyMap<string, string> {
  const out = new Map<string, string>();
  for (const property of Array.from(style)) {
    out.set(property, style.getPropertyValue(property));
  }
  return out;
}

function isStyleRule(rule: CSSRule): rule is CSSStyleRule {
  return "selectorText" in rule && "style" in rule;
}
