// Package logsafe strips line breaks from values that reach the agent's
// logs from outside it: a remote service's error text, a model name from a
// request path, the path of an HTTP request. slog's handlers already quote
// such values, but that is a property of the handler in use, not of the
// value; a line break inside a logged value is what lets one request forge
// a second log line, and this makes sure none survives to the handler
// whatever it is. CodeQL's go/log-injection query recognises the
// replacement as a sanitizer, so a logged value that passed through here
// is not an alert.
package logsafe

import "strings"

// String returns s with every carriage return and newline made visible as
// its escape sequence, so the value stays on one log line and nothing it
// carried is lost.
func String(s string) string {
	s = strings.ReplaceAll(s, "\r", "\\r")
	return strings.ReplaceAll(s, "\n", "\\n")
}

// Err is String over err's message. A nil error reads as "<nil>", the way
// slog would have rendered it.
func Err(err error) string {
	if err == nil {
		return "<nil>"
	}
	return String(err.Error())
}
