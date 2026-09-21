import { activityPageSchema, taskMovePreviewResponseSchema } from "./workflowBoard";

describe("Manual Move schema", () => {
  it("preserves whitespace in resolved Manual Move values", () => {
    const resolvedValue = "  indented code\n ";
    const parsed = taskMovePreviewResponseSchema.parse({
      outcome: "transition",
      transition: {
        choices: [
          {
            transition_key: "next",
            label: "Next",
            source_node_display_name: "Plan",
            required_values: [
              {
                node_key: "plan",
                output_name: "summary",
                description: "Summary",
                resolved_value: resolvedValue,
              },
            ],
          },
        ],
      },
    });
    expect(parsed).toMatchObject({
      transition: { choices: [{ requiredValues: [{ resolvedValue }] }] },
    });
  });

  it("requires Manual Move descriptions to be explicit nullable nonblank metadata", () => {
    const base = {
      outcome: "transition" as const,
      transition: {
        choices: [
          {
            transition_key: "next",
            label: "Next",
            source_node_display_name: "Plan",
            required_values: [
              { node_key: "plan", output_name: "summary", resolved_value: null },
            ],
          },
        ],
      },
    };
    const baseChoice = base.transition.choices[0];
    if (baseChoice === undefined) throw new Error("expected base choice");
    const parsed = taskMovePreviewResponseSchema.parse({
      ...base,
      transition: {
        ...base.transition,
        choices: [
          { ...baseChoice, required_values: [{ ...baseChoice.required_values[0], description: null }] },
        ],
      },
    });
    if (parsed.outcome !== "transition" || !("transition" in parsed)) {
      throw new Error("expected transition preview");
    }
    expect(parsed.transition.choices[0]?.requiredValues[0]?.description).toBeNull();
    expect(() =>
      taskMovePreviewResponseSchema.parse({
        ...base,
        transition: {
          ...base.transition,
          choices: [
            { ...baseChoice, required_values: [{ ...baseChoice.required_values[0], description: " \t" }] },
          ],
        },
      }),
    ).toThrow();
    expect(() => taskMovePreviewResponseSchema.parse(base)).toThrow();
  });
});

describe("task activity schema", () => {
  const commentActivity = {
    activity_id: "activity-comment-1",
    type: "comment",
    task_id: "task-1",
    occurred_at_unix_ms: 1,
    updated_at_unix_ms: 1,
    comment: {
      id: "comment-1",
      task_id: "task-1",
      body: "Operator note",
      author: "user",
      created_at_unix_ms: 1,
      updated_at_unix_ms: 1,
    },
  };
  const sessionStartedActivity = {
    activity_id: "activity-session-1",
    type: "session_started",
    task_id: "task-1",
    occurred_at_unix_ms: 2,
    updated_at_unix_ms: 2,
    session_started: { session_id: "session-1", name: "Implementation" },
  };

  it("accepts only comment and session-started activity variants", () => {
    expect(
      activityPageSchema.parse({ items: [commentActivity, sessionStartedActivity], next_offset: 50 }),
    ).toMatchObject({
      items: [
        { type: "comment", comment: { id: "comment-1" } },
        { type: "session_started", sessionID: "session-1", sessionName: "Implementation" },
      ],
    });
  });

  it("rejects removed persistence and history activity variants", () => {
    const legacyActivities = [
      { ...commentActivity, type: "run_interrupted", run: { id: "run-1" } },
      { ...commentActivity, type: "transition_applied", transition: { id: "transition-1" } },
      { ...commentActivity, type: "comment", actor: "GUI", summary: "Comment added" },
      { ...sessionStartedActivity, type: "session_started", history: { id: "history-1" } },
    ];
    for (const item of legacyActivities) {
      expect(() => activityPageSchema.parse({ items: [item], next_offset: null })).toThrow();
    }
  });

  it("rejects Activity cursor and generated-at fields", () => {
    expect(() =>
      activityPageSchema.parse({ items: [], next_page_token: "legacy", generated_at_unix_ms: 2 }),
    ).toThrow();
  });
});
