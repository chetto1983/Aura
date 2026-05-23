package api

import (
	"errors"
	"net/http"
	"os"
)

// writeReadError handles the "not-found vs server-error" branch that repeats
// across every resource-read handler. Returns true when err != nil so the
// caller can `return` immediately without further checks.
//
// Usage: if writeReadError(w, deps, err, "thing not found", "api: op", "failed to read", "id", id) { return }
func writeReadError(w http.ResponseWriter, deps Deps, err error, notFoundMsg, warnMsg, serverMsg string, logFields ...any) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, deps.Logger, http.StatusNotFound, notFoundMsg)
		return true
	}
	deps.Logger.Warn(warnMsg, append(logFields, "error", err)...)
	writeError(w, deps.Logger, http.StatusInternalServerError, serverMsg)
	return true
}
