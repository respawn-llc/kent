package workflowstore

import (
	"errors"
	"reflect"
	"testing"

	"core/server/workflow"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func TestWorkflowGraphSaveTransactionConstraints(t *testing.T) {
	const invalidReference = `INSERT INTO workflow_node_groups (id, workflow_id, group_key, sort_order)
		VALUES ('c3d410e6-4f35-4911-8a32-be54ae2d7ba2',
			x'79063994e39940faac0162d77f28ee9d', 'injected_group', 0);`
	for _, test := range []struct {
		name        string
		triggerBody string
		repair      bool
		wantCode    *int
	}{
		{
			name:        "temporary foreign key violation",
			triggerBody: invalidReference,
			repair:      true,
		},
		{
			name:        "foreign key violation at commit",
			triggerBody: invalidReference,
			wantCode:    new(int(sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY)),
		},
		{
			name:        "statement constraint failure",
			triggerBody: `SELECT RAISE(ABORT, 'injected graph save constraint failure');`,
			wantCode:    new(int(sqlite3.SQLITE_CONSTRAINT_TRIGGER)),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newGraphSaveFixture(t, createValidWorkflow)
			req := f.request(f.record.Version, false, f.def)
			req.Nodes = renameWorkflowGraphSaveNode(req.Nodes, testNodeID("node-agent-"+f.workflowID.String()), "Edited Agent")
			req.Metadata = &WorkflowGraphSaveMetadata{Name: "Edited workflow", Description: "Edited description"}

			// Inject failures at actual graph-save writes without adding production hooks.
			if _, err := f.store.db.ExecContext(f.ctx, `CREATE TEMP TRIGGER graph_save_constraint
				AFTER UPDATE OF version ON workflows BEGIN `+test.triggerBody+` END`); err != nil {
				t.Fatal(err)
			}
			if test.repair {
				// Metadata follows the graph write and repairs the temporary FK violation.
				if _, err := f.store.db.ExecContext(f.ctx, `CREATE TEMP TRIGGER graph_save_repair
					AFTER UPDATE OF name ON workflows BEGIN
					DELETE FROM workflow_node_groups WHERE id = 'c3d410e6-4f35-4911-8a32-be54ae2d7ba2';
					END`); err != nil {
					t.Fatal(err)
				}
			}

			result, err := f.store.SaveWorkflowGraph(f.ctx, req)
			if test.repair {
				if err != nil || !result.Saved {
					t.Fatalf("save with repaired references: result=%+v err=%v", result, err)
				}
				def, record := f.current(t)
				if record.Version != f.record.Version+1 || record.Name != req.Metadata.Name ||
					workflow.NodeDisplayName(nodeByID(t, def, testNodeID("node-agent-"+f.workflowID.String()))) != "Edited Agent" {
					t.Fatalf("save did not persist graph and metadata: definition=%+v record=%+v", def, record)
				}
				return
			}
			var constraint *sqlite.Error
			if !errors.As(err, &constraint) || constraint.Code() != *test.wantCode || result.Saved {
				t.Fatalf("save: result=%+v err=%v, want SQLite code %d", result, err, *test.wantCode)
			}
			def, record := f.current(t)
			if !reflect.DeepEqual(def, f.def) || !reflect.DeepEqual(record, f.record) {
				t.Fatalf("failed save changed graph or metadata: definition=%+v record=%+v", def, record)
			}
			if _, err := f.store.db.ExecContext(f.ctx, `DROP TRIGGER graph_save_constraint`); err != nil {
				t.Fatal(err)
			}
			if retried := f.save(t, req); !retried.Saved || retried.Version != f.record.Version+1 {
				t.Fatalf("save after rollback: %+v", retried)
			}
		})
	}
}
