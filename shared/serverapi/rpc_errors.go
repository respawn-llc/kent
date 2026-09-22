package serverapi

import "errors"

const TimeoutErrorCode = "timeout"

var ErrMethodNotFound = errors.New("rpc method not found")
