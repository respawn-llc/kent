package session

import (
	"errors"

	"core/shared/config"
)

func (s *Store) AdoptToolSelection(selection *config.ToolSelection) error {
	if selection == nil {
		return nil
	}
	if err := ValidateRetainedToolSelection(selection); err != nil {
		return err
	}
	return s.mutateAndPersist(func() error {
		if s.meta.RetainedToolSelection == nil {
			s.meta.RetainedToolSelection = config.CloneToolSelection(selection)
		}
		return nil
	})
}

func ValidateRetainedToolSelection(selection *config.ToolSelection) error {
	if selection == nil {
		return nil
	}
	if selection.Origin.RetainedSessionID != nil {
		return errors.New("a retained tool selection must store its original declaration, not a Session projection")
	}
	return selection.Validate()
}
