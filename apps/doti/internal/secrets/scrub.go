package secrets

import "strings"

// Scrubber redacts known secret values from anything on its way to a log,
// a terminal or an error.
//
// This exists because the natural way to write an error in this package
// interpolates the thing that went wrong, and here the thing that went wrong
// is often the value itself - a template that failed to parse, a write that
// failed halfway. One `%v` in the wrong place puts a database password in a
// CI log, and nothing about the code would look wrong.
//
// Short values are ignored deliberately. Redacting a two-character secret
// would blank out unrelated substrings of every message and make failures
// unreadable, which trades one bad outcome for another.
type Scrubber struct {
	values []string
}

// MinRedactable is the shortest value worth masking.
const MinRedactable = 6

// Add registers a value to redact. Safe to call with anything, including
// values that turn out to be empty.
func (s *Scrubber) Add(value string) {
	if len(value) < MinRedactable {
		return
	}
	for _, known := range s.values {
		if known == value {
			return
		}
	}
	s.values = append(s.values, value)
}

// Text returns the string with every known value replaced.
func (s *Scrubber) Text(in string) string {
	out := in
	for _, value := range s.values {
		out = strings.ReplaceAll(out, value, "[redacted]")
	}
	return out
}

// Err rewrites an error's message with every known value replaced.
//
// The original is deliberately not wrapped: errors.As/Is on a scrubbed error
// could otherwise reach the unscrubbed message through Unwrap, which is the
// same leak by a longer route.
func (s *Scrubber) Err(err error) error {
	if err == nil {
		return nil
	}
	cleaned := s.Text(err.Error())
	if cleaned == err.Error() {
		return err
	}
	return scrubbedError{msg: cleaned}
}

type scrubbedError struct{ msg string }

func (e scrubbedError) Error() string { return e.msg }
