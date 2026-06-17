package authui

import (
	"context"

	"core/shared/auth"
)

type AuthStateLoader interface {
	Load(context.Context) (auth.State, error)
}

func AuthSlashCommandName(ctx context.Context, loader AuthStateLoader) (string, error) {
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
