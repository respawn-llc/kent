-- +goose Down

DROP INDEX IF EXISTS task_comments_task_visible_updated_idx;
DROP TABLE IF EXISTS task_comments;

DROP INDEX IF EXISTS task_transition_edges_target_placement_idx;
DROP INDEX IF EXISTS task_transition_edges_workflow_edge_idx;
DROP INDEX IF EXISTS task_transition_edges_transition_state_idx;
DROP TABLE IF EXISTS task_transition_edges;

DROP INDEX IF EXISTS task_transitions_task_created_idx;
DROP TABLE IF EXISTS task_transitions;

DROP INDEX IF EXISTS task_runs_task_created_idx;
DROP INDEX IF EXISTS task_runs_outcome_idx;
DROP INDEX IF EXISTS task_runs_runnable_idx;
DROP INDEX IF EXISTS task_runs_session_idx;
DROP INDEX IF EXISTS task_runs_placement_idx;
DROP TABLE IF EXISTS task_runs;

DROP INDEX IF EXISTS task_node_placements_parallel_batch_idx;
DROP INDEX IF EXISTS task_node_placements_node_state_idx;
DROP INDEX IF EXISTS task_node_placements_task_state_idx;
DROP TABLE IF EXISTS task_node_placements;

DROP INDEX IF EXISTS tasks_project_short_id_idx;
DROP INDEX IF EXISTS tasks_project_updated_idx;
DROP INDEX IF EXISTS tasks_managed_worktree_idx;
DROP INDEX IF EXISTS tasks_project_workflow_link_idx;
DROP TABLE IF EXISTS tasks;

DROP INDEX IF EXISTS project_workflow_links_workflow_idx;
DROP INDEX IF EXISTS project_workflow_links_project_active_idx;
DROP INDEX IF EXISTS project_workflow_links_default_idx;
DROP INDEX IF EXISTS project_workflow_links_active_workflow_idx;
DROP TABLE IF EXISTS project_workflow_links;

DROP INDEX IF EXISTS workflow_edges_target_node_idx;
DROP INDEX IF EXISTS workflow_edges_transition_group_sort_idx;
DROP TABLE IF EXISTS workflow_edges;

DROP INDEX IF EXISTS workflow_transition_groups_source_transition_idx;
DROP TABLE IF EXISTS workflow_transition_groups;

DROP INDEX IF EXISTS workflow_nodes_workflow_sort_idx;
DROP INDEX IF EXISTS workflow_nodes_one_start_idx;
DROP TABLE IF EXISTS workflow_nodes;

DROP TABLE IF EXISTS workflows;

DROP INDEX IF EXISTS sessions_worktree_updated_idx;
DROP INDEX IF EXISTS projects_project_key_idx;

ALTER TABLE projects DROP COLUMN next_task_seq;
ALTER TABLE projects DROP COLUMN project_key;
