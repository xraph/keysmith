package contract

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/key"
)

const maxRevokeReasonLength = 500

type keysRevokeRequest struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

type keyIDRequest struct {
	ID string `json:"id"`
}

// keyResponse answers a command that changes a key and carries no secret.
type keyResponse struct {
	Key KeySummary `json:"key"`
}

// Messages for the state a command refuses. The handler checks the state
// before it calls the engine, and uses the same text when the engine refuses
// because another request got there first.
const (
	msgAlreadyRevoked    = "this key is already revoked"
	msgSuspendNeedsLive  = "only an active key can be suspended"
	msgReactivateNeedsOn = "only a suspended key can be reactivated"
)

func stateConflict(message string) error {
	return &dashcontract.Error{Code: dashcontract.CodeConflict, Message: message}
}

// keyStateChange is what five commands share: revoke, suspend, reactivate,
// and the two scope commands, assign and remove. It resolves the caller,
// runs validate, loads the key for the tenant, refuses by state, calls
// apply, and answers the key as stored afterwards. None of them carries a
// secret, so a failed reload after the engine call can answer an error.
//
// validate runs before the load and before apply. The scope commands rely on
// that order: their validate closure is where `names` is filled, and their
// apply closure reads it.
func keyStateChange(
	ctx context.Context, deps Deps, p dashcontract.Principal,
	intent, rawID, conflict string,
	allowed func(k *key.Key, now time.Time) bool,
	apply func(ctx context.Context, kid id.KeyID) error,
	validate func() error,
	mapErr func(intent string, err error) error,
) (keyResponse, error) {
	tenant, err := tenantFrom(p, deps)
	if err != nil {
		return keyResponse{}, err
	}
	if _, uerr := requireUser(p); uerr != nil {
		return keyResponse{}, uerr
	}
	if validate != nil {
		if verr := validate(); verr != nil {
			return keyResponse{}, verr
		}
	}
	k, err := loadKeyForTenant(ctx, deps, tenant, rawID, intent)
	if err != nil {
		return keyResponse{}, err
	}
	if !allowed(k, time.Now()) {
		return keyResponse{}, stateConflict(conflict)
	}
	if aerr := apply(ctx, k.ID); aerr != nil {
		if errors.Is(aerr, keysmith.ErrInvalidStateTransition) {
			return keyResponse{}, stateConflict(conflict)
		}
		return keyResponse{}, mapErr(intent, aerr)
	}
	kid := k.ID
	k, err = loadKeyForTenant(ctx, deps, tenant, kid.String(), intent)
	if err != nil {
		return keyResponse{}, err
	}
	names, err := scopeNames(ctx, deps.Engine, kid)
	if err != nil {
		return keyResponse{}, deps.mapError(intent, err)
	}
	return keyResponse{Key: projectKey(k, names, time.Now())}, nil
}

func keysRevokeHandler(deps Deps) func(context.Context, keysRevokeRequest, dashcontract.Principal) (keyResponse, error) {
	return func(ctx context.Context, in keysRevokeRequest, p dashcontract.Principal) (keyResponse, error) {
		reason := strings.TrimSpace(in.Reason)
		return keyStateChange(ctx, deps, p, "keys.revoke", in.ID, msgAlreadyRevoked,
			func(k *key.Key, now time.Time) bool {
				state, _ := effectiveState(k, now)
				return state != string(key.StateRevoked)
			},
			func(ctx context.Context, kid id.KeyID) error {
				return deps.Engine.RevokeKey(ctx, kid, reason)
			},
			func() error {
				switch {
				case reason == "":
					return badRequest("reason is required")
				case utf8.RuneCountInString(reason) > maxRevokeReasonLength:
					return badRequest("reason must be at most 500 characters")
				}
				return nil
			}, deps.mapError)
	}
}

func keysSuspendHandler(deps Deps) func(context.Context, keyIDRequest, dashcontract.Principal) (keyResponse, error) {
	return func(ctx context.Context, in keyIDRequest, p dashcontract.Principal) (keyResponse, error) {
		return keyStateChange(ctx, deps, p, "keys.suspend", in.ID, msgSuspendNeedsLive,
			// The effective state, so a key that is active in the store but
			// past its expiry is refused here and never reaches the engine.
			func(k *key.Key, now time.Time) bool {
				state, _ := effectiveState(k, now)
				return state == string(key.StateActive)
			},
			deps.Engine.SuspendKey, nil, deps.mapError)
	}
}

func keysReactivateHandler(deps Deps) func(context.Context, keyIDRequest, dashcontract.Principal) (keyResponse, error) {
	return func(ctx context.Context, in keyIDRequest, p dashcontract.Principal) (keyResponse, error) {
		return keyStateChange(ctx, deps, p, "keys.reactivate", in.ID, msgReactivateNeedsOn,
			func(k *key.Key, _ time.Time) bool {
				return k.State == key.StateSuspended && k.RevokedAt == nil
			},
			deps.Engine.ReactivateKey, nil, deps.mapError)
	}
}
