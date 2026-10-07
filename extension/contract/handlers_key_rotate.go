package contract

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/xraph/forge"
	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/rotation"
)

const (
	// maxGraceSeconds is 90 days, the longest grace window the dashboard
	// will ask for.
	maxGraceSeconds = 7776000

	// defaultGrace is the engine's own default when neither the request nor
	// the key's policy names a window.
	defaultGrace = 24 * time.Hour
)

type keysRotateRequest struct {
	ID           string `json:"id"`
	Reason       string `json:"reason"`       // manual | compromise | policy
	GraceSeconds *int64 `json:"graceSeconds"` // absent or null: policy grace, else 24h
}

// keysRotateResponse carries a raw key. Like keys.create it is returned once
// and nothing stores it.
type keysRotateResponse struct {
	Key          KeySummary    `json:"key"`
	RawKey       string        `json:"rawKey"`
	PreviousKeys []PreviousKey `json:"previousKeys"` // every window open after this rotation, never nil
}

type keysEndGraceRequest struct {
	ID string `json:"id"`
}

type keysEndGraceResponse struct {
	Key    KeySummary `json:"key"`
	Closed int64      `json:"closed"`
}

func keysRotateHandler(deps Deps) func(context.Context, keysRotateRequest, dashcontract.Principal) (keysRotateResponse, error) {
	return func(ctx context.Context, in keysRotateRequest, p dashcontract.Principal) (keysRotateResponse, error) {
		const intent = "keys.rotate"
		tenant, err := tenantFrom(ctx, p, deps)
		if err != nil {
			return keysRotateResponse{}, err
		}
		subject, err := requireUser(p)
		if err != nil {
			return keysRotateResponse{}, err
		}

		// "scheduled" is the engine's own reason for automatic rotations, so
		// the dashboard cannot claim it.
		switch rotation.Reason(in.Reason) {
		case rotation.ReasonManual, rotation.ReasonCompromise, rotation.ReasonPolicy:
		default:
			return keysRotateResponse{}, badRequest("reason must be one of manual, compromise, policy")
		}
		if in.GraceSeconds != nil && (*in.GraceSeconds < 0 || *in.GraceSeconds > maxGraceSeconds) {
			return keysRotateResponse{}, badRequest("graceSeconds must be between 0 and 7776000")
		}

		// Every read happens before RotateKey. Once the engine has returned
		// the new raw key, nothing that can fail may cost the caller that key:
		// the old one would be on a clock and the new one lost.
		k, err := loadKeyForTenant(ctx, deps, tenant, in.ID, intent)
		if err != nil {
			return keysRotateResponse{}, err
		}
		// RotateKey rewrites the key it loads, and a store may hand back
		// shared state, so keep what the response needs by value.
		keyID, oldHint := k.ID, k.Hint

		// Resolve the window here, the way the engine would, so the response
		// can describe it without another read. A missing policy means none;
		// any other failure stops before anything is rotated. Like the
		// engine, this does not check the policy's tenant: only its grace is
		// used, and it is never shown.
		grace := defaultGrace
		switch {
		case in.GraceSeconds != nil:
			grace = time.Duration(*in.GraceSeconds) * time.Second
		case k.PolicyID != nil:
			pol, perr := deps.Engine.GetPolicy(ctx, *k.PolicyID)
			switch {
			case perr == nil:
				if pol.GracePeriod > 0 {
					grace = pol.GracePeriod
				}
			case !errors.Is(perr, keysmith.ErrPolicyNotFound):
				return keysRotateResponse{}, deps.mapError(intent, perr)
			}
		}

		names, err := scopeNames(ctx, deps.Engine, keyID)
		if err != nil {
			return keysRotateResponse{}, deps.mapError(intent, err)
		}
		earlier, err := listOpenWindows(ctx, deps.Engine, keyID, time.Now())
		if err != nil {
			return keysRotateResponse{}, deps.mapError(intent, err)
		}

		result, err := deps.Engine.RotateKey(ctx, keyID, rotation.Reason(in.Reason),
			keysmith.WithGrace(grace), keysmith.WithRotatedBy(subject))
		if err != nil {
			if errors.Is(err, keysmith.ErrInvalidStateTransition) {
				return keysRotateResponse{}, &dashcontract.Error{
					Code:    dashcontract.CodeConflict,
					Message: "a revoked or expired key cannot be rotated",
				}
			}
			return keysRotateResponse{}, deps.mapError(intent, err)
		}

		// From here on the handler must answer with the raw key.
		out := keysRotateResponse{
			Key:    projectKey(result.Key, names, time.Now()),
			RawKey: result.RawKey,
		}
		windows, werr := listOpenWindows(ctx, deps.Engine, keyID, time.Now())
		if werr != nil {
			// The rotation is done and the key must reach the caller. Log
			// the read error (never the key) and build the windows from what
			// was read before plus the one this rotation opened.
			if deps.Logger != nil {
				deps.Logger.Error("keysmith/contract: could not list windows after a rotation",
					forge.F("intent", intent),
					forge.F("key_id", keyID.String()),
					forge.F("error", werr),
				)
			}
			windows = earlier
			if grace > 0 && oldHint != "" && result.Key.RotatedAt != nil {
				windows = append(windows, PreviousKey{
					Hint:      oldHint,
					Reason:    in.Reason,
					RotatedAt: rfc3339(*result.Key.RotatedAt),
					GraceEnds: rfc3339(result.Key.RotatedAt.Add(grace)),
				})
				sort.SliceStable(windows, func(i, j int) bool { return windows[i].GraceEnds < windows[j].GraceEnds })
			}
		}
		if windows == nil {
			windows = []PreviousKey{}
		}
		out.PreviousKeys = windows
		return out, nil
	}
}

func keysEndGraceHandler(deps Deps) func(context.Context, keysEndGraceRequest, dashcontract.Principal) (keysEndGraceResponse, error) {
	return func(ctx context.Context, in keysEndGraceRequest, p dashcontract.Principal) (keysEndGraceResponse, error) {
		const intent = "keys.endGrace"
		tenant, err := tenantFrom(ctx, p, deps)
		if err != nil {
			return keysEndGraceResponse{}, err
		}
		if _, uerr := requireUser(p); uerr != nil {
			return keysEndGraceResponse{}, uerr
		}
		k, err := loadKeyForTenant(ctx, deps, tenant, in.ID, intent)
		if err != nil {
			return keysEndGraceResponse{}, err
		}
		closed, err := deps.Engine.EndGrace(ctx, k.ID)
		if err != nil {
			return keysEndGraceResponse{}, deps.mapError(intent, err)
		}
		// Answer the key as stored now, not the copy read before the write.
		k, err = loadKeyForTenant(ctx, deps, tenant, in.ID, intent)
		if err != nil {
			return keysEndGraceResponse{}, err
		}
		names, err := scopeNames(ctx, deps.Engine, k.ID)
		if err != nil {
			return keysEndGraceResponse{}, deps.mapError(intent, err)
		}
		return keysEndGraceResponse{Key: projectKey(k, names, time.Now()), Closed: closed}, nil
	}
}
