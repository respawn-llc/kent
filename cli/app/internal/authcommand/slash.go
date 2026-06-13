package authcommand

import (
	"context"

	"core/shared/auth"
)

type StateLoader interface {
	Load(context.Context) (auth.State, error)
}

func SlashCommandName(ctx context.Context, loader StateLoader) (string, error) {
	if loader == nil {
		return "login", nil
	}
	state, err := loader.Load(ctx)
	if err != nil {
		return "", err
	}
	if state.Method.Type == auth.MethodOAuth {
		return "logout", nil
	}
	return "login", nil
}
