package sessionnaming

import (
	"context"
	"errors"
	"log/slog"

	"core/server/agentruntime"
	"core/server/llm"
	"core/server/nano"
	"core/server/sessionruntime"
	"core/server/sessionstore"
	"core/shared/config"
	"core/shared/runtimeids"
)

type Service struct {
	nano          *nano.Service
	authority     *sessionruntime.Authority
	publish       func(context.Context, string)
	failure       func(context.Context, string, error)
	resolveConfig func(string) (config.App, error)
}

func NewService(inference *nano.Service, authority *sessionruntime.Authority, resolveConfig func(string) (config.App, error), publish func(context.Context, string), failure func(context.Context, string, error)) *Service {
	return &Service{nano: inference, authority: authority, resolveConfig: resolveConfig, publish: publish, failure: failure}
}

func (s *Service) Submit(id runtimeids.SessionID, root string, input runtime.SessionNamingInput) {
	app, err := s.resolveConfig(root)
	if err != nil {
		s.report(context.Background(), id.String(), err, input.Headless)
		return
	}
	settings := app.Settings
	if settings.Nano == nil || !settings.Nano.SessionNaming {
		return
	}
	invoke, err := s.nano.Admit()
	if err != nil {
		s.report(context.Background(), id.String(), err, input.Headless)
		return
	}
	prepare := func(target llm.NanoTarget) (llm.Request, error) {
		return PrepareRequest(target, input.Task, input.Commentary)
	}
	work := func(ctx context.Context) error {
		response, err := invoke(ctx, settings, prepare)
		if err == nil {
			if response.Assistant.Content == nil {
				err = errors.New("session-naming result has no text")
			} else {
				err = s.save(ctx, id.String(), *response.Assistant.Content)
			}
		}
		if err != nil {
			s.report(ctx, id.String(), err, input.Headless)
		}
		return err
	}
	if _, err := s.authority.AcceptLifecycleTask(work); err != nil {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		// An accepted admission always runs, including rejected shutdown work,
		// so its waiting reservation or active permit is released.
		_, releaseErr := invoke(ctx, settings, prepare)
		s.report(ctx, id.String(), errors.Join(err, releaseErr), input.Headless)
	}
}

func (s *Service) save(ctx context.Context, id string, summary string) error {
	var mutation session.NameMutationResult
	err := s.authority.WithSessionChatSettings(ctx, id, func(_ context.Context, store *session.Store, _ *runtime.Engine) (bool, error) {
		meta := store.Meta()
		identity := store.Identity()
		title, err := InterpretTitle(summary, meta.EffectiveCategory(), identity.ShortID)
		if err != nil {
			return false, err
		}
		mutation, err = store.MutateNameIfUnnamed(title)
		return false, err
	})
	if mutation.Committed {
		s.publish(context.WithoutCancel(ctx), id)
	}
	return err
}

func (s *Service) report(ctx context.Context, id string, err error, headless bool) {
	slog.ErrorContext(ctx, "Automatic Session naming failed", "session_id", id, "error", err)
	if !headless && s.failure != nil {
		s.failure(ctx, id, err)
	}
}
