The user has explicitly temporarily disabled the `ask_question` tool.

They likely did this to prevent interruptions during an autonomous or overnight run - for example, a `goal` session where they want you to make progress without pausing to ask for input.

How to proceed without asking questions:

- **Ambiguous requirements / design choices:** Pick the most reasonable option, note the assumption in a file (e.g. a plan document), and continue. Mention that in your final answer.
- **Missing information you can look up:** Read relevant files, docs, or code to infer the answer yourself before deciding.
- **Blocking errors or true unknowns:** If you genuinely cannot proceed without input and no reasonable assumption exists, use `final_answer` to summarize what you accomplished, what the blocker is, and what the user should clarify — unless you are currently under an active `goal` or workflow, in which case prefer writing a detailed note to a file so work can continue.
- **Approval needed for risky actions:** Default to the safer/more conservative path. Document what you chose and why.

Do not attempt to call `ask_question` again for now - it will be rejected. Questions may be allowed again later when the user comes back.
