package auth

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/oauth2"
)

type OAuthTokenSource interface {
	Token() (*oauth2.Token, error)
}

type OAuthTokenSourceFactory interface {
	TokenSource(ctx context.Context, current oauth2.Token) OAuthTokenSource
}

type OAuthRefresher struct {
	Factory       OAuthTokenSourceFactory
	Now           func() time.Time
	RefreshBefore time.Duration
	Refresh       func(ctx context.Context, method Method) (Method, error)
}

func NewOAuthRefresher(factory OAuthTokenSourceFactory, now func() time.Time, refreshBefore time.Duration) *OAuthRefresher {
	if now == nil {
		now = time.Now
	}
	if refreshBefore < 0 {
		refreshBefore = 0
	}
	return &OAuthRefresher{
		Factory:       factory,
		Now:           now,
		RefreshBefore: refreshBefore,
	}
}

func (r *OAuthRefresher) MaybeRefresh(ctx context.Context, method Method) (Method, bool, error) {
	if method.Type != MethodOAuth {
		return method, false, nil
	}
	if err := method.Validate(); err != nil {
		return Method{}, false, err
	}
	if r == nil {
		return method, false, nil
	}

	now := r.Now().UTC()
	expiry := method.OAuth.Expiry.UTC()
	if expiry.IsZero() || expiry.After(now.Add(r.RefreshBefore)) {
		return method, false, nil
	}
	if r.Refresh != nil {
		updated, err := r.Refresh(ctx, method)
		if err != nil {
			return Method{}, false, err
		}
		return updated, true, nil
	}
	if r.Factory == nil {
		return Method{}, false, ErrMissingOAuthFactory
	}

	src := r.Factory.TokenSource(ctx, oauth2.Token{
		AccessToken:  method.OAuth.AccessToken,
		RefreshToken: method.OAuth.RefreshToken,
		TokenType:    method.OAuth.TokenType,
		Expiry:       method.OAuth.Expiry,
	})
	if src == nil {
		return Method{}, false, fmt.Errorf("%w: token source is nil", ErrOAuthRefreshFailed)
	}

	tok, err := src.Token()
	if err != nil {
		return Method{}, false, fmt.Errorf("%w: %v", ErrOAuthRefreshFailed, err)
	}
	if tok == nil || tok.AccessToken == "" {
		return Method{}, false, fmt.Errorf("%w: missing access token in refresh response", ErrOAuthRefreshFailed)
	}

	updated := method
	updated.OAuth = &OAuthMethod{
		AccessToken:  tok.AccessToken,
		RefreshToken: method.OAuth.RefreshToken,
		TokenType:    method.OAuth.TokenType,
		Expiry:       tok.Expiry.UTC(),
	}
	if tok.RefreshToken != "" {
		updated.OAuth.RefreshToken = tok.RefreshToken
	}
	if tok.TokenType != "" {
		updated.OAuth.TokenType = tok.TokenType
	}
	if updated.OAuth.TokenType == "" {
		updated.OAuth.TokenType = "Bearer"
	}
	return updated, true, nil
}
