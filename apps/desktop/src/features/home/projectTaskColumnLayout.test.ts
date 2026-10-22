import { describe, expect, it } from "bun:test";

import { resolveProjectTaskVisibleColumns, type ProjectTaskColumnLayout } from "./projectTaskColumnLayout";

const layout: ProjectTaskColumnLayout = {
  dependenciesPx: 48,
  idCharacters: 8,
  labelsPx: 320,
  stagePx: 80,
  workflowPx: 96,
};

describe("Project Task responsive columns", () => {
  it("shrinks Labels after hiding Workflow and before hiding Labels", () => {
    const visible = resolveProjectTaskVisibleColumns(592, layout, true);

    expect(visible).toEqual({
      dependencies: true,
      labelsPx: 228,
      stage: true,
      title: true,
      workflow: false,
    });
  });

  it("keeps the minimum usable Labels track until the remaining width no longer fits it", () => {
    expect(resolveProjectTaskVisibleColumns(412, layout, true)).toMatchObject({
      dependencies: true,
      labelsPx: 48,
      stage: true,
      title: true,
      workflow: false,
    });
    expect(resolveProjectTaskVisibleColumns(411, layout, true)).toMatchObject({
      dependencies: true,
      labelsPx: null,
      stage: true,
      title: true,
      workflow: false,
    });
  });

  it("hides Workflow, Labels, Stage, then Dependencies as width contracts", () => {
    expect(resolveProjectTaskVisibleColumns(1000, layout, true)).toMatchObject({
      workflow: true,
      labelsPx: 320,
      stage: true,
      dependencies: true,
    });
    expect(resolveProjectTaskVisibleColumns(700, layout, true)).toMatchObject({
      workflow: false,
      labelsPx: 320,
      stage: true,
      dependencies: true,
    });
    expect(resolveProjectTaskVisibleColumns(352, layout, true)).toMatchObject({
      workflow: false,
      labelsPx: null,
      stage: true,
      dependencies: true,
    });
    expect(resolveProjectTaskVisibleColumns(351, layout, true)).toMatchObject({
      workflow: false,
      labelsPx: null,
      stage: false,
      dependencies: true,
    });
    expect(resolveProjectTaskVisibleColumns(259, layout, true)).toMatchObject({
      workflow: false,
      labelsPx: null,
      stage: false,
      dependencies: false,
    });
  });

  it("hides Workflow when every Task belongs to the sole linked Workflow", () => {
    expect(resolveProjectTaskVisibleColumns(null, layout, false)).toMatchObject({
      workflow: false,
    });
    expect(resolveProjectTaskVisibleColumns(null, layout, true)).toMatchObject({
      workflow: true,
    });
  });
});
