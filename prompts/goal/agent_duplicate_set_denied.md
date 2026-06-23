Overwriting an existing goal is not allowed. Continue working on the current goal. If it is fully complete, run `{{.LaunchCommand}} goal complete`. If you need to inspect it again, run `{{.LaunchCommand}} goal show`. If you are blocked or unable to complete the goal, use `ask_question` to ask the user for help. Do not call `{{.LaunchCommand}} goal set` again while this goal remains active.

Current goal (status: {{.Status}}):
<goal>
{{.Objective}}
</goal>
