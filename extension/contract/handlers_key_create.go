package contract

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/key"
)

const maxKeyNameLength = 200

// keyPrefixPattern has no underscore on purpose. The default generator writes
// prefix_environment_random, so an underscore in the prefix would make the
// parts of the key ambiguous.
var keyPrefixPattern = regexp.MustCompile(`^[a-z][a-z0-9]{1,15}$`)

type keysCreateRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Environment string   `json:"environment"`
	Prefix      string   `json:"prefix"`
	PolicyID    string   `json:"policyId"`
	Scopes      []string `json:"scopes"`
	ExpiresAt   string   `json:"expiresAt"` // RFC3339 or ""
}

// keyWithSecretResponse is the only response in the contract that carries a
// raw key. It is returned once, from keys.create, and nothing stores it.
type keyWithSecretResponse struct {
	Key    KeySummary `json:"key"`
	RawKey string     `json:"rawKey"`
}

// scopeNameFromError pulls the scope name out of the engine's wrapped
// `scope "<name>": ...` error. It reports false when the text has no such
// prefix.
func scopeNameFromError(err error) (string, bool) {
	rest, ok := strings.CutPrefix(err.Error(), "scope ")
	if !ok {
		return "", false
	}
	quoted, qerr := strconv.QuotedPrefix(rest)
	if qerr != nil {
		return "", false
	}
	name, uerr := strconv.Unquote(quoted)
	if uerr != nil {
		return "", false
	}
	return name, true
}

// mapScopeError answers the scope errors the engine returns from a create or
// an assignment with messages the form can show. Anything else goes through
// mapError, so an unexpected error stays generic.
func (d Deps) mapScopeError(intent string, err error) error {
	switch {
	case errors.Is(err, keysmith.ErrScopeNotFound):
		if name, ok := scopeNameFromError(err); ok {
			return badRequest("scope " + strconv.Quote(name) + " does not exist in this tenant")
		}
		return badRequest("a scope does not exist in this tenant")
	case errors.Is(err, keysmith.ErrScopeNotAllowed):
		return badRequest("a scope is outside this policy's allowed scopes")
	default:
		return d.mapError(intent, err)
	}
}

// mapCreateError answers the engine errors a create can hit with messages the
// form can show. Scope errors go through mapScopeError.
func (d Deps) mapCreateError(intent string, err error) error {
	switch {
	case errors.Is(err, keysmith.ErrPolicyNotFound):
		// The policy went away between the handler's check and the engine's read.
		return badRequest("policy not found")
	case errors.Is(err, keysmith.ErrKeyLifetimeExceeded):
		return badRequest("expiresAt is beyond this policy's maximum key lifetime")
	default:
		return d.mapScopeError(intent, err)
	}
}

func keysCreateHandler(deps Deps) func(context.Context, keysCreateRequest, dashcontract.Principal) (keyWithSecretResponse, error) {
	return func(ctx context.Context, in keysCreateRequest, p dashcontract.Principal) (keyWithSecretResponse, error) {
		const intent = "keys.create"
		tenant, err := tenantFrom(p, deps)
		if err != nil {
			return keyWithSecretResponse{}, err
		}
		app, err := appFrom(p, deps)
		if err != nil {
			return keyWithSecretResponse{}, err
		}
		subject, err := requireUser(p)
		if err != nil {
			return keyWithSecretResponse{}, err
		}

		name := strings.TrimSpace(in.Name)
		if name == "" {
			return keyWithSecretResponse{}, badRequest("name is required")
		}
		if utf8.RuneCountInString(name) > maxKeyNameLength {
			return keyWithSecretResponse{}, badRequest("name is too long")
		}

		switch in.Environment {
		case string(key.EnvLive), string(key.EnvTest), string(key.EnvStaging):
		default:
			return keyWithSecretResponse{}, badRequest("environment must be one of live, test, staging")
		}

		if !keyPrefixPattern.MatchString(in.Prefix) {
			return keyWithSecretResponse{}, badRequest(
				"prefix must be 2 to 16 lowercase letters or digits, starting with a letter")
		}

		var expiresAt *time.Time
		if in.ExpiresAt != "" {
			t, perr := time.Parse(time.RFC3339, in.ExpiresAt)
			if perr != nil || !t.After(time.Now()) {
				return keyWithSecretResponse{}, badRequest("expiresAt must be an RFC3339 timestamp in the future")
			}
			t = t.UTC()
			expiresAt = &t
		}

		var policyID *id.PolicyID
		if in.PolicyID != "" {
			// One message for a malformed ID, a missing policy and another
			// tenant's policy, so a foreign ID is never confirmed. The engine
			// does not check the policy's tenant: this is the guard.
			pid, perr := id.ParsePolicyID(in.PolicyID)
			if perr != nil {
				return keyWithSecretResponse{}, badRequest("policy not found")
			}
			pol, gerr := deps.Engine.GetPolicy(ctx, pid)
			switch {
			case errors.Is(gerr, keysmith.ErrPolicyNotFound):
				return keyWithSecretResponse{}, badRequest("policy not found")
			case gerr != nil:
				return keyWithSecretResponse{}, deps.mapError(intent, gerr)
			case pol.TenantID != tenant:
				return keyWithSecretResponse{}, badRequest("policy not found")
			}
			policyID = &pid
		}

		created, err := deps.Engine.CreateKey(engineCtx(ctx, app, tenant), &keysmith.CreateKeyInput{
			Name:        name,
			Description: in.Description,
			Prefix:      in.Prefix,
			Environment: key.Environment(in.Environment),
			PolicyID:    policyID,
			Scopes:      in.Scopes,
			CreatedBy:   subject,
			TenantID:    tenant,
			ExpiresAt:   expiresAt,
		})
		if err != nil {
			return keyWithSecretResponse{}, deps.mapCreateError(intent, err)
		}

		// Nothing that can fail may sit between a successful CreateKey and this
		// return: a failure here would lose the raw key and leave a live key
		// nobody holds. The engine returns nil only after every name resolved
		// in this tenant and was assigned, so the stored set is the input with
		// duplicates removed. created.Key.Scopes is the raw input, duplicates
		// included, so it is not used.
		names := slices.Compact(slices.Sorted(slices.Values(in.Scopes)))
		return keyWithSecretResponse{
			Key:    projectKey(created.Key, names, time.Now()),
			RawKey: created.RawKey,
		}, nil
	}
}
