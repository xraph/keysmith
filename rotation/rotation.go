// Package rotation defines key rotation records and reasons.
package rotation

import (
	"time"

	"github.com/xraph/keysmith/id"
)

// Reason indicates why a rotation occurred.
type Reason string

const (
	// ReasonScheduled indicates a policy-driven auto-rotation.
	ReasonScheduled Reason = "scheduled"

	// ReasonManual indicates a user-initiated rotation.
	ReasonManual Reason = "manual"

	// ReasonCompromise indicates a rotation due to a security incident.
	ReasonCompromise Reason = "compromise"

	// ReasonPolicy indicates a rotation forced by a policy change.
	ReasonPolicy Reason = "policy"
)

// Record tracks a key rotation event.
type Record struct {
	ID         id.RotationID `json:"id" db:"id"`
	KeyID      id.KeyID      `json:"key_id" db:"key_id"`
	TenantID   string        `json:"tenant_id" db:"tenant_id"`
	OldKeyHash string        `json:"-" db:"old_key_hash"`
	NewKeyHash string        `json:"-" db:"new_key_hash"`
	Reason     Reason        `json:"reason" db:"reason"`
	GraceTTL   time.Duration `json:"grace_ttl" db:"grace_ttl_ms"`
	GraceEnds  time.Time     `json:"grace_ends" db:"grace_ends"`
	RotatedBy  string        `json:"rotated_by,omitempty" db:"rotated_by"`
	CreatedAt  time.Time     `json:"created_at" db:"created_at"`
}

// ListFilter contains filters for listing rotation records.
type ListFilter struct {
	KeyID    *id.KeyID `json:"key_id,omitempty"`
	TenantID string    `json:"tenant_id,omitempty"`
	Reason   Reason    `json:"reason,omitempty"`
	Limit    int       `json:"limit,omitempty"`
	Offset   int       `json:"offset,omitempty"`
}
