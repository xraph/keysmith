package contract

import (
	"errors"

	"github.com/xraph/forge"
	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/usage"
)

// msgKeyConflict answers a key command whose write lost a race.
const msgKeyConflict = "this key changed while you were acting on it. Reload and try again."

// mapError translates a keysmith error into a *dashcontract.Error the
// dashboard client can branch on.
//
// Most BAD_REQUEST and CONFLICT messages are the error's own text: those
// errors carry only names the caller supplied. An error that is not one of
// the sentinels below becomes INTERNAL with a generic message, because its
// text can carry a connection string or a stored value. Handlers call it
// through Deps.mapError, which also logs the INTERNAL case server-side.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, keysmith.ErrKeyNotFound):
		return &dashcontract.Error{Code: dashcontract.CodeNotFound, Message: "key not found"}
	case errors.Is(err, keysmith.ErrPolicyNotFound):
		return &dashcontract.Error{Code: dashcontract.CodeNotFound, Message: "policy not found"}
	case errors.Is(err, keysmith.ErrRotationNotFound):
		return &dashcontract.Error{Code: dashcontract.CodeNotFound, Message: "rotation not found"}
	case errors.Is(err, keysmith.ErrScopeNotFound),
		errors.Is(err, keysmith.ErrScopeNotAllowed),
		errors.Is(err, keysmith.ErrKeyLifetimeExceeded),
		errors.Is(err, usage.ErrInvalidPeriod):
		return &dashcontract.Error{Code: dashcontract.CodeBadRequest, Message: err.Error()}
	case errors.Is(err, keysmith.ErrPolicyNameTaken):
		// The engine's text names the tenant, which the dashboard never shows.
		return &dashcontract.Error{Code: dashcontract.CodeConflict, Message: "a policy with this name already exists"}
	case errors.Is(err, keysmith.ErrScopeNameTaken):
		// The engine's text names the tenant here too.
		return &dashcontract.Error{Code: dashcontract.CodeConflict, Message: "a scope with this name already exists"}
	case errors.Is(err, keysmith.ErrInvalidStateTransition),
		errors.Is(err, keysmith.ErrPolicyInUse):
		return &dashcontract.Error{Code: dashcontract.CodeConflict, Message: err.Error()}
	case errors.Is(err, keysmith.ErrKeyConflict):
		// Another write landed between the engine's read and its write.
		return &dashcontract.Error{Code: dashcontract.CodeConflict, Message: msgKeyConflict}
	default:
		return &dashcontract.Error{Code: dashcontract.CodeInternal, Message: "an internal error occurred"}
	}
}

// mapError maps err exactly as the package-level mapError does and, when
// the result is INTERNAL and d.Logger is set, logs the underlying error at
// Error level with the intent that hit it. That is the only case an
// operator cannot diagnose from what the client sees. The client still gets
// only the generic message.
func (d Deps) mapError(intent string, err error) error {
	mapped := mapError(err)
	if d.Logger == nil || mapped == nil {
		return mapped
	}
	var ce *dashcontract.Error
	if errors.As(mapped, &ce) && ce.Code == dashcontract.CodeInternal {
		d.Logger.Error("keysmith/contract: internal error answering intent",
			forge.F("intent", intent),
			forge.F("error", err),
		)
	}
	return mapped
}
