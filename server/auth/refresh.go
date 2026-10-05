package auth

import (
	"context"
	"fmt"
	"time"
)

type OAuthRefresher struct {
	now           func() time.Time
	refreshBefore time.Duration
	refresh       func(ctx context.Context, method OAuthMethod) (OAuthMethod, error)
}

func NewOAuthRefresher(now func() time.Time, refreshBefore time.Duration, refresh func(context.Context, OAuthMethod) (OAuthMethod, error)) *OAuthRefresher {
	if now == nil {
		now = time.Now
	}
	if refreshBefore < 0 {
		refreshBefore = 0
	}
	return &OAuthRefresher{
		now:           now,
		refreshBefore: refreshBefore,
		refresh:       refresh,
	}
}

func (r *OAuthRefresher) MaybeRefresh(ctx context.Context, method OAuthMethod) (OAuthMethod, bool, error) {
	if err := method.Validate(); err != nil {
		return OAuthMethod{}, false, err
	}
	if r == nil {
		return method, false, nil
	}
	if r.now == nil {
		return OAuthMethod{}, false, fmt.Errorf("%w: refresh clock is required", ErrOAuthRefreshFailed)
	}

	now := r.now().UTC()
	expiry := method.Expiry.UTC()
	if expiry.IsZero() || expiry.After(now.Add(r.refreshBefore)) {
		return method, false, nil
	}
	if r.refresh == nil {
		return OAuthMethod{}, false, fmt.Errorf("%w: refresh operation is required", ErrOAuthRefreshFailed)
	}
	updated, err := r.refresh(ctx, method)
	if err != nil {
		return OAuthMethod{}, false, err
	}
	return updated, true, nil
}
