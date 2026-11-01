package speech

import (
	"context"
	"errors"
	"slices"
	"time"

	"core/server/launch"
	"core/server/llm"
	"core/shared/config"
)

type Limits struct {
	Encodings   []llm.SpeechEncoding
	MaxBytes    int64
	MaxDuration time.Duration
}

type Selection struct {
	settings config.Settings
	Limits   *Limits
}

type Invocation struct {
	ID         config.ConnectionID
	Definition config.ProviderConnection
	Profile    llm.SpeechProfile
}

// EligibleDefaults is a read-only projection. Allocation remains owned by the
// same ConnectionRotation used for launches.
func EligibleDefaults(settings config.Settings) (Selection, error) {
	members, err := settings.ConnectionMembers()
	if err != nil {
		var missing *config.ConnectionReferenceError
		if errors.As(err, &missing) {
			return Selection{}, nil
		}
		return Selection{}, err
	}
	var limits *Limits
	eligible := make(config.ConnectionSelection, 0, len(members))
	for _, id := range members {
		variant, err := llm.ResolveConnectionVariant(settings.Connections[id])
		if err != nil {
			return Selection{}, err
		}
		profile := variant.Variant.Speech
		if profile == nil {
			continue
		}
		eligible = append(eligible, id)
		if limits == nil {
			limits = &Limits{Encodings: slices.Clone(profile.Encodings), MaxBytes: profile.MaxBytes, MaxDuration: profile.MaxDuration}
		} else {
			limits.MaxBytes = min(limits.MaxBytes, profile.MaxBytes)
			limits.MaxDuration = min(limits.MaxDuration, profile.MaxDuration)
			limits.Encodings = slices.DeleteFunc(limits.Encodings, func(encoding llm.SpeechEncoding) bool {
				return !slices.Contains(profile.Encodings, encoding)
			})
		}
	}
	if limits == nil || len(limits.Encodings) == 0 {
		return Selection{}, nil
	}
	settings.Connection = &eligible
	return Selection{settings: settings, Limits: limits}, nil
}

func (s Selection) Allocate(ctx context.Context, rotation *launch.ConnectionRotation) (Invocation, error) {
	if s.Limits == nil {
		return Invocation{}, errors.New("no speech-capable default connection")
	}
	id, err := rotation.Prepare(ctx, s.settings, true)
	if err != nil {
		return Invocation{}, err
	}
	definition := s.settings.Connections[id]
	variant, err := llm.ResolveConnectionVariant(definition)
	if err != nil {
		return Invocation{}, err
	}
	profile := *variant.Variant.Speech
	if profile.Model != nil && definition.SpeechToTextModel != nil {
		profile.Model = definition.SpeechToTextModel
	}
	return Invocation{ID: id, Definition: definition, Profile: profile}, nil
}
