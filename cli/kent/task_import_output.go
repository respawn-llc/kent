package main

import (
	taskimportpb "core/shared/protoapi/gen/kent/api/task_import"
	"core/shared/serverapi"
	"encoding/json"
	"errors"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"io"
	"strconv"
)

type importOutput struct {
	stdout   io.Writer
	stderr   io.Writer
	json     bool
	account  *taskimportpb.Account
	started  bool
	hasItem  bool
	writeErr error
	request  *taskimportpb.Request
}

func (o *importOutput) write(value string) {
	if o.writeErr == nil {
		_, o.writeErr = io.WriteString(o.stdout, value)
	}
}
func (o *importOutput) message(value proto.Message) {
	data, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(value)
	if err != nil {
		o.writeErr = err
		return
	}
	o.write(string(data))
}
func (o *importOutput) begin() {
	if o.started {
		return
	}
	o.started = true
	o.write(`{"account":`)
	if o.account == nil {
		o.write("null")
	} else {
		o.message(o.account)
	}
	o.write(`,"results":[`)
}
func (o *importOutput) PublishTaskImportProgress(event *taskimportpb.ProgressEvent) {
	if account := event.GetAccount(); account != nil {
		o.account = account
		if !o.json {
			o.write(fmt.Sprintf("GitHub account: @%s\n", account.Login))
		}
		return
	}
	item := event.GetItem()
	if item == nil {
		return
	}
	if o.json {
		o.begin()
		if o.hasItem {
			o.write(",")
		}
		o.message(item)
		o.hasItem = true
		return
	}
	source := importReferenceLabel(item.Reference)
	if item.SourceUrl != nil {
		source = *item.SourceUrl
	}
	task := ""
	if item.Task != nil {
		task = fmt.Sprintf(" %s (%s)", item.Task.ShortId, item.Task.TaskId)
	}
	switch outcome := item.Outcome.(type) {
	case *taskimportpb.Item_Created:
		o.write(fmt.Sprintf("Created%s ← %s\n", task, source))
	case *taskimportpb.Item_Skipped:
		o.write(fmt.Sprintf("Skipped%s ← %s\n", task, source))
	case *taskimportpb.Item_Failed:
		if selection := outcome.Failed.GetWorkflowSelection(); selection != nil {
			o.write(fmt.Sprintf("Failed%s ← %s [%s]\n", task, source, importPhaseLabel(outcome.Failed.Phase)))
			recovery, err := taskImportRecoveryForSelectionError(selection, taskImportCommandContext{Request: o.request, SourceURL: item.GetSourceUrl()})
			if err != nil {
				fmt.Fprintln(o.stderr, err)
			} else {
				writeTaskWorkflowSelectionRecovery(o.stderr, recovery)
			}
		} else {
			o.write(fmt.Sprintf("Failed%s ← %s [%s]: %s\n", task, source, importPhaseLabel(outcome.Failed.Phase), outcome.Failed.Diagnostic))
		}
	}
	for _, warning := range item.Warnings {
		fmt.Fprintln(o.stderr, warning)
	}
}
func (o *importOutput) finish(summary *taskimportpb.Success, observationErr error) int {
	var rejected *serverapi.TaskImportError
	knownFailure := errors.As(observationErr, &rejected)
	if o.json {
		o.begin()
		o.write(`],"summary":`)
		if summary == nil {
			o.write("null")
		} else {
			o.message(summary)
		}
		if knownFailure {
			o.write(`,"error":`)
			o.message(rejected.Detail)
		} else if observationErr != nil {
			data, err := json.Marshal(observationErr.Error())
			if err != nil {
				o.writeErr = err
			} else {
				o.write(`,"observation_error":` + string(data))
			}
		}
		o.write("}\n")
	} else if knownFailure {
		fmt.Fprintf(o.stderr, "Import failed [%s]: %s\n", importPhaseLabel(rejected.Detail.Phase), rejected.Detail.Diagnostic)
	} else if observationErr != nil {
		fmt.Fprintf(o.stderr, "Import observation failed; admitted work may still be running. Do not assume resubmitting is safe: %v\n", observationErr)
	} else if summary != nil {
		o.write(fmt.Sprintf("%d created, %d skipped, %d failed\n", summary.Created, summary.Skipped, summary.Failed))
	}
	if o.writeErr != nil {
		fmt.Fprintln(o.stderr, o.writeErr)
		return 1
	}
	if observationErr != nil || summary != nil && summary.Failed > 0 {
		return 1
	}
	return 0
}

func importReferenceLabel(reference *taskimportpb.Reference) string {
	switch value := reference.Value.(type) {
	case *taskimportpb.Reference_IssueUrl:
		return value.IssueUrl
	case *taskimportpb.Reference_Number:
		return strconv.FormatInt(value.Number, 10)
	case *taskimportpb.Reference_Range:
		return fmt.Sprintf("%d-%d", value.Range.First, value.Range.Last)
	default:
		return "invalid issue reference"
	}
}

func importPhaseLabel(phase taskimportpb.FailurePhase) string {
	switch phase {
	case taskimportpb.FailurePhase_FAILURE_PHASE_RESOLUTION:
		return "resolution"
	case taskimportpb.FailurePhase_FAILURE_PHASE_CREDENTIALS:
		return "credentials"
	case taskimportpb.FailurePhase_FAILURE_PHASE_ISSUE:
		return "issue"
	case taskimportpb.FailurePhase_FAILURE_PHASE_FIELDS:
		return "issue fields"
	case taskimportpb.FailurePhase_FAILURE_PHASE_LABELS:
		return "labels"
	case taskimportpb.FailurePhase_FAILURE_PHASE_CREATION:
		return "task creation"
	case taskimportpb.FailurePhase_FAILURE_PHASE_COMMENTS:
		return "comments"
	default:
		return fmt.Sprintf("invalid failure phase %d", phase)
	}
}
