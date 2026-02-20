// Package store defines the composite store interface for Keysmith.
package store

import (
	"context"

	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/rotation"
	"github.com/xraph/keysmith/scope"
	"github.com/xraph/keysmith/usage"
)

// Store composes all Keysmith subsystem stores via accessor methods.
// Implementations must provide all subsystem stores plus lifecycle methods.
type Store interface {
	// Keys returns the key store.
	Keys() key.Store

	// Policies returns the policy store.
	Policies() policy.Store

	// Usages returns the usage store.
	Usages() usage.Store

	// Rotations returns the rotation store.
	Rotations() rotation.Store

	// Scopes returns the scope store.
	Scopes() scope.Store

	// Migrate runs database migrations.
	Migrate(ctx context.Context) error

	// Ping checks database connectivity.
	Ping(ctx context.Context) error

	// Close releases database resources.
	Close() error
}
