package runtimewire

import (
	"context"
	"fmt"
	"strings"

	"core/server/agentruntime"
	"core/server/runtimeactivity"
	"core/shared/invariant"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
)

type RuntimeReadModelPublisher interface {
	PublishRuntimeReadModelUpdate(sessionID string, update *runtimepb.ReadModelUpdate)
	RuntimeActivityRegistrySnapshot(sessionID string) runtimeactivity.RegistrySnapshot
	RuntimeReadModelFeedSnapshot(ctx context.Context, sessionID string) (*runtimepb.ReadModelUpdate, error)
}

func NewStepLifecycleSink(sessionID string, publisher RuntimeReadModelPublisher) runtime.StepLifecycleSink {
	return NewStepLifecycleSinkWithInvariantPolicy(sessionID, publisher, invariant.NewPolicy())
}

func NewStepLifecycleSinkWithInvariantPolicy(sessionID string, publisher RuntimeReadModelPublisher, policy invariant.Policy) runtime.StepLifecycleSink {
	if publisher == nil {
		return nil
	}
	return stepLifecycleSink{sessionID: strings.TrimSpace(sessionID), publisher: publisher, policy: policy}
}

type stepLifecycleSink struct {
	sessionID string
	publisher RuntimeReadModelPublisher
	policy    invariant.Policy
}

func (s stepLifecycleSink) StepBegan(_ context.Context, snapshot runtime.StepLifecycleSnapshot) error {
	return s.publish("step_began", false, snapshot)
}

func (s stepLifecycleSink) StepEnded(_ context.Context, snapshot runtime.StepLifecycleSnapshot) error {
	return s.publish("step_ended", true, snapshot)
}

func (s stepLifecycleSink) publish(cause string, terminal bool, lifecycle runtime.StepLifecycleSnapshot) error {
	sessionID := s.sessionID
	snapshot, err := s.publisher.RuntimeReadModelFeedSnapshot(context.Background(), sessionID)
	if err != nil {
		return s.handlePublicationFailure(cause, terminal, lifecycle, err)
	}
	if snapshot.Activity.State == runtimepb.ActivityState_RUNTIME_ACTIVITY_UNAVAILABLE {
		snapshot.Activity = &runtimepb.Activity{
			State:          runtimepb.ActivityState_RUNTIME_ACTIVITY_REGISTERED_IDLE,
			Reviewer:       runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INACTIVE,
			QueueAccepting: true,
		}
	}
	s.publisher.PublishRuntimeReadModelUpdate(sessionID, snapshot)
	return nil
}

func (s stepLifecycleSink) handlePublicationFailure(cause string, terminal bool, lifecycle runtime.StepLifecycleSnapshot, failure error) error {
	sessionID := s.sessionID
	s.policy.Check(false, invariant.ReadModelPublicationDiagnostic(invariant.ReadModelPublicationDiagnosticInput{
		Operation:        "runtime_step_lifecycle",
		SessionID:        sessionID,
		PublicationCause: cause,
		ResolverInputs:   fmt.Sprintf("%+v", lifecycle),
		OwnerSnapshots:   fmt.Sprintf("%+v", s.publisher.RuntimeActivityRegistrySnapshot(sessionID)),
		ProviderError:    failure.Error(),
	}))
	if !terminal {
		return nil
	}
	recovery, err := s.publisher.RuntimeReadModelFeedSnapshot(context.Background(), sessionID)
	if err != nil {
		return err
	}
	s.publisher.PublishRuntimeReadModelUpdate(sessionID, recovery)
	return nil
}
