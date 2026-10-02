package contract

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/rotation"
)

// openWindowPage is how many rotation records listOpenWindows asks for per
// call.
const openWindowPage = 100

// requireID trims raw and parses it as a key ID. Empty answers BAD_REQUEST
// "id is required"; unparseable answers BAD_REQUEST "id is not a key id".
func requireID(raw string) (id.KeyID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return id.KeyID{}, badRequest("id is required")
	}
	kid, err := id.ParseKeyID(raw)
	if err != nil {
		return id.KeyID{}, badRequest("id is not a key id")
	}
	return kid, nil
}

// loadKeyForTenant loads the key and answers keyNotFound() when it does not
// exist OR belongs to another tenant, so the two are indistinguishable.
// Every by-ID handler, read or write, goes through it.
func loadKeyForTenant(ctx context.Context, deps Deps, tenant, rawID string) (*key.Key, error) {
	kid, err := requireID(rawID)
	if err != nil {
		return nil, err
	}
	k, err := deps.Engine.GetKey(ctx, kid)
	if err != nil {
		if errors.Is(err, keysmith.ErrKeyNotFound) {
			return nil, keyNotFound()
		}
		return nil, deps.mapError("keys.load", err)
	}
	if k.TenantID != tenant {
		return nil, keyNotFound()
	}
	return k, nil
}

// listOpenWindows pages through ListRotations for the key until a short
// page, keeping records with a non-empty OldHint and GraceEnds after now,
// sorted by GraceEnds ascending. There is no cap: the slice 2 detail read
// stopped at 100 and could hide a still-valid previous key.
func listOpenWindows(ctx context.Context, eng *keysmith.Engine, keyID id.KeyID, now time.Time) ([]PreviousKey, error) {
	var open []*rotation.Record
	for offset := 0; ; offset += openWindowPage {
		recs, err := eng.ListRotations(ctx, &rotation.ListFilter{KeyID: &keyID, Limit: openWindowPage, Offset: offset})
		if err != nil {
			return nil, err
		}
		for _, r := range recs {
			// Records written before the grace fix have no old hint and must
			// never read as an open window.
			if r.OldHint != "" && r.GraceEnds.After(now) {
				open = append(open, r)
			}
		}
		if len(recs) < openWindowPage {
			break
		}
	}
	sort.SliceStable(open, func(i, j int) bool { return open[i].GraceEnds.Before(open[j].GraceEnds) })
	out := make([]PreviousKey, 0, len(open))
	for _, r := range open {
		out = append(out, PreviousKey{
			RotationID: r.ID.String(),
			Hint:       r.OldHint,
			Reason:     string(r.Reason),
			RotatedAt:  rfc3339(r.CreatedAt),
			GraceEnds:  rfc3339(r.GraceEnds),
		})
	}
	return out, nil
}
