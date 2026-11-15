package sync

import "errors"

var (
	errNoProvider = errors.New("sync: no provider registered for account")
	errBadPayload = errors.New("sync: op payload is missing or malformed")
	errNoLabels   = errors.New("sync: provider does not support labels")
)
