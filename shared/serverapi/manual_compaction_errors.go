package serverapi

var ErrManualCompactionTooSoon = &ManualCompactionError{}
var ErrManualCompactionDisabled = &ManualCompactionDisabledError{}
var ErrManualCompactionActive = &ManualCompactionActiveError{}

type ManualCompactionError struct{}

func (*ManualCompactionError) Error() string { return "manual compaction is too soon" }

type ManualCompactionDisabledError struct{}

func (*ManualCompactionDisabledError) Error() string { return "manual compaction is disabled" }

type ManualCompactionActiveError struct{}

func (*ManualCompactionActiveError) Error() string { return "manual compaction is already active" }
