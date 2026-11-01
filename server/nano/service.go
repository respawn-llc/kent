package nano

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"core/server/authservice"
	"core/server/launch"
	"core/server/llm"
	"core/server/runtimewire"
	"core/shared/config"

	"github.com/google/uuid"
	"golang.org/x/sync/semaphore"
)

var ErrUnavailable = errors.New("nano is not configured")

type BusyError struct{}

func (*BusyError) Error() string { return "nano waiting capacity exhausted" }

type PrepareRequest func(llm.NanoTarget) (llm.Request, error)
type Invocation func(context.Context, config.Settings, PrepareRequest) (llm.Response, error)

type Service struct {
	permits     *semaphore.Weighted
	waiting     atomic.Int32
	rotation    *launch.ConnectionRotation
	connections *authservice.ConnectionResolver
	factory     runtimewire.RuntimeClientFactory
}

func NewService(concurrency int, rotation *launch.ConnectionRotation, connections *authservice.ConnectionResolver, factory runtimewire.RuntimeClientFactory) (*Service, error) {
	if concurrency < 1 || rotation == nil || connections == nil {
		return nil, errors.New("nano requires positive concurrency, shared rotation and connection resolver")
	}
	return &Service{permits: semaphore.NewWeighted(int64(concurrency)), rotation: rotation, connections: connections, factory: factory}, nil
}

// Admit reserves capacity before the caller retains or launches background work.
// Each accepted invocation must be called exactly once, even during shutdown.
func (s *Service) Admit() (Invocation, error) {
	held := s.permits.TryAcquire(1)
	if !held {
		for {
			waiting := s.waiting.Load()
			if waiting >= 100 {
				return nil, &BusyError{}
			}
			if s.waiting.CompareAndSwap(waiting, waiting+1) {
				break
			}
		}
	}
	return func(ctx context.Context, settings config.Settings, prepare PrepareRequest) (llm.Response, error) {
		if !held {
			err := s.permits.Acquire(ctx, 1)
			s.waiting.Add(-1)
			if err != nil {
				return llm.Response{}, err
			}
		}
		defer s.permits.Release(1)
		if err := ctx.Err(); err != nil {
			return llm.Response{}, err
		}
		if settings.Nano == nil {
			return llm.Response{}, ErrUnavailable
		}
		lifetimeContext := ctx
		ctx, cancel := context.WithTimeout(ctx, time.Duration(settings.Nano.TimeoutSeconds)*time.Second)
		defer cancel()
		targets, err := llm.ResolveNanoTargets(settings)
		if err != nil {
			return llm.Response{}, err
		}
		selected := settings
		selected.Connection = settings.Nano.Connection
		id, err := s.rotation.Prepare(ctx, selected, true)
		if err != nil {
			return llm.Response{}, err
		}
		for _, target := range targets {
			targetID, err := target.Settings.Connection.ConcreteID()
			if err != nil {
				return llm.Response{}, err
			}
			if *targetID == id {
				return s.generate(ctx, lifetimeContext, target, prepare)
			}
		}
		return llm.Response{}, fmt.Errorf("selected nano connection %q is not resolved", id)
	}, nil
}

func (s *Service) generate(ctx, lifetimeContext context.Context, target llm.NanoTarget, prepare PrepareRequest) (_ llm.Response, resultErr error) {
	connection, err := s.connections.Resolve(target.Settings)
	if err != nil {
		return llm.Response{}, err
	}
	identity := uuid.NewString()
	client, err := runtimewire.NewRuntimeClient(lifetimeContext, s.factory, runtimewire.RuntimeClientRequest{
		RecordUsage: s.rotation.RecordUsage,
		Purpose:     runtimewire.RuntimeClientPurposeNano, SessionID: identity,
		ActiveSettings: target.Settings, Connection: connection, RequestCapabilities: target.Capabilities,
	})
	if err != nil {
		return llm.Response{}, err
	}
	defer func() {
		if closer, ok := client.(io.Closer); ok {
			resultErr = errors.Join(resultErr, closer.Close())
		}
	}()
	request, err := prepare(target)
	if err != nil {
		return llm.Response{}, err
	}
	request.Model = target.Settings.Model
	request.ReasoningEffort = target.Settings.ThinkingLevel
	request.SupportsReasoningEffort = target.Settings.ModelCapabilities.SupportsReasoningEffort
	request.SessionID = &identity
	request.CodexDispatch = nil
	if connection.Definition.Protocol == config.ConnectionChatGPT {
		request.CodexDispatch, err = llm.NewCodexDispatchContext(llm.CodexDispatchFacts{
			SessionID: identity, StepID: identity, CompactionGeneration: 0, RequestKind: llm.CodexRequestKindTurn.Optional(),
		})
		if err != nil {
			return llm.Response{}, err
		}
	}
	if err := request.Validate(); err != nil {
		return llm.Response{}, err
	}
	if err := llm.ValidateToolChoiceSupport(target.Capabilities, request.ToolChoiceMode); err != nil {
		return llm.Response{}, err
	}
	if err := llm.ValidateVisionInputs(request.Items, target.SupportsVisionInputs); err != nil {
		return llm.Response{}, err
	}
	if request.StructuredOutput != nil && !target.Capabilities.SupportsResponsesAPI {
		return llm.Response{}, fmt.Errorf("%w: nano provider does not support structured output", llm.ErrInvalidRequest)
	}
	if request.EnableNativeWebSearch && !target.Capabilities.SupportsNativeWebSearch ||
		request.FastMode && !target.Capabilities.SupportsFastMode {
		return llm.Response{}, fmt.Errorf("%w: nano provider does not support requested controls", llm.ErrInvalidRequest)
	}
	input, err := llm.EstimateRequestTokens(target.Estimator, request)
	if err != nil {
		return llm.Response{}, err
	}
	if input+request.MaxTokens > target.Settings.ModelContextWindow {
		return llm.Response{}, fmt.Errorf("%w: nano input and output exceed model context window", llm.ErrInvalidRequest)
	}
	return llm.GenerateWithRetry(ctx,
		func(ctx context.Context, callbacks llm.StreamCallbacks, finished func()) (llm.Response, error) {
			response, err := client.Generate(ctx, request, callbacks)
			finished()
			return response, err
		}, llm.GenerationRetryOptions{Schedule: llm.DefaultGenerationRetrySchedule()})
}
