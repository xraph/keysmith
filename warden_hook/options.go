package wardenhook

import log "github.com/xraph/go-utils/log"

// Option is a functional option for the Warden bridge extension.
type Option func(*Extension)

// WithAutoAssign controls whether a Warden role is auto-assigned on key creation.
func WithAutoAssign(v bool) Option { return func(e *Extension) { e.autoAssign = v } }

// WithDefaultRole sets the default Warden role slug for API keys.
func WithDefaultRole(slug string) Option { return func(e *Extension) { e.defaultRole = slug } }

// WithLogger sets the logger.
func WithLogger(l log.Logger) Option { return func(e *Extension) { e.logger = l } }
