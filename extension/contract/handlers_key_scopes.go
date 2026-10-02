package contract

import (
	"context"
	"slices"
	"strings"
	"time"

	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/key"
)

const msgRevokedScopes = "a revoked key's scopes cannot be changed"

type keysScopesRequest struct {
	ID     string   `json:"id"`
	Scopes []string `json:"scopes"`
}

// scopesToChange trims every name, drops blanks and duplicates, and refuses an
// empty result.
func scopesToChange(raw []string) ([]string, error) {
	names := make([]string, 0, len(raw))
	for _, n := range raw {
		if n = strings.TrimSpace(n); n != "" {
			names = append(names, n)
		}
	}
	slices.Sort(names)
	names = slices.Compact(names)
	if len(names) == 0 {
		return nil, badRequest("scopes must name at least one scope")
	}
	return names, nil
}

// keysScopesChange is the shared shape of assign and remove. The revoked
// check goes by the effective state, so a key with RevokedAt set but a stale
// stored state is refused too.
func keysScopesChange(
	deps Deps, intent string,
	change func(ctx context.Context, kid id.KeyID, names []string) error,
) func(context.Context, keysScopesRequest, dashcontract.Principal) (keyResponse, error) {
	return func(ctx context.Context, in keysScopesRequest, p dashcontract.Principal) (keyResponse, error) {
		var names []string
		return keyStateChange(ctx, deps, p, intent, in.ID, msgRevokedScopes,
			func(k *key.Key, now time.Time) bool {
				state, _ := effectiveState(k, now)
				return state != string(key.StateRevoked)
			},
			func(ctx context.Context, kid id.KeyID) error {
				return change(ctx, kid, names)
			},
			func() (err error) {
				names, err = scopesToChange(in.Scopes)
				return err
			},
			deps.mapScopeError)
	}
}

func keysScopesAssignHandler(deps Deps) func(context.Context, keysScopesRequest, dashcontract.Principal) (keyResponse, error) {
	return keysScopesChange(deps, "keys.scopes.assign", deps.Engine.AssignScopes)
}

func keysScopesRemoveHandler(deps Deps) func(context.Context, keysScopesRequest, dashcontract.Principal) (keyResponse, error) {
	return keysScopesChange(deps, "keys.scopes.remove", deps.Engine.RemoveScopes)
}
