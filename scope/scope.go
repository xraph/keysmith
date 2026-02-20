// Package scope defines permission scopes that can be assigned to API keys.
package scope

import (
	"time"

	"github.com/xraph/keysmith/id"
)

// Scope defines a permission scope that can be assigned to keys.
// Scopes are hierarchical: "read:users" is a child of "read".
type Scope struct {
	ID          id.ScopeID     `json:"id" db:"id"`
	TenantID    string         `json:"tenant_id" db:"tenant_id"`
	AppID       string         `json:"app_id" db:"app_id"`
	Name        string         `json:"name" db:"name"`
	Description string         `json:"description,omitempty" db:"description"`
	Parent      string         `json:"parent,omitempty" db:"parent"`
	Metadata    map[string]any `json:"metadata,omitempty" db:"metadata"`
	CreatedAt   time.Time      `json:"created_at" db:"created_at"`
}

// ListFilter contains filters for listing scopes.
type ListFilter struct {
	TenantID string `json:"tenant_id,omitempty"`
	Parent   string `json:"parent,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Offset   int    `json:"offset,omitempty"`
}
