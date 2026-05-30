SELECT target_placement_id
FROM task_transition_edges
WHERE task_transition_id = ? AND target_placement_id IS NOT NULL
ORDER BY rowid ASC
