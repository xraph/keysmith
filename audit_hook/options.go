package audithook

import "log/slog"

// Option configures an Extension.
type Option func(*Extension)

// WithEnabled restricts audit recording to the specified actions only.
// If not called, all actions are recorded.
func WithEnabled(actions ...string) Option {
	return func(e *Extension) {
		e.enabled = make(map[string]bool, len(actions))
		for _, a := range actions {
			e.enabled[a] = true
		}
	}
}

// WithLogger sets the logger for recording errors.
func WithLogger(logger *slog.Logger) Option {
	return func(e *Extension) {
		e.logger = logger
	}
}
