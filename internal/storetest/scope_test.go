package storetest_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/internal/storetest"
	"github.com/xraph/keysmith/scope"
	"github.com/xraph/keysmith/store"
)

func TestDeletingAScopeRemovesItFromEveryKey(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		k := newKey("t1")
		require.NoError(t, s.Keys().Create(ctx, k))
		sc := &scope.Scope{ID: id.NewScopeID(), TenantID: "t1", AppID: "app1", Name: "billing:read", CreatedAt: time.Now()}
		require.NoError(t, s.Scopes().Create(ctx, sc))
		require.NoError(t, s.Scopes().AssignToKey(ctx, k.ID, []string{"billing:read"}))

		require.NoError(t, s.Scopes().Delete(ctx, sc.ID))
		held, err := s.Scopes().ListByKey(ctx, k.ID)
		require.NoError(t, err)
		require.Empty(t, held, "a deleted scope is no longer held")

		again := &scope.Scope{ID: id.NewScopeID(), TenantID: "t1", AppID: "app1", Name: "billing:read", CreatedAt: time.Now()}
		require.NoError(t, s.Scopes().Create(ctx, again))
		held, err = s.Scopes().ListByKey(ctx, k.ID)
		require.NoError(t, err)
		require.Empty(t, held, "re-creating the name does not re-grant it")
	})
}

// Deleting a scope takes its name off keys in its own tenant only. A key in
// another tenant that holds a same-name scope of its own keeps it.
func TestDeletingAScopeLeavesAnotherTenantsSameNameScope(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		k2 := newKey("t2")
		require.NoError(t, s.Keys().Create(ctx, k2))
		mine := &scope.Scope{ID: id.NewScopeID(), TenantID: "t1", AppID: "app1", Name: "billing:read", CreatedAt: time.Now()}
		theirs := &scope.Scope{ID: id.NewScopeID(), TenantID: "t2", AppID: "app1", Name: "billing:read", CreatedAt: time.Now()}
		require.NoError(t, s.Scopes().Create(ctx, mine))
		require.NoError(t, s.Scopes().Create(ctx, theirs))
		require.NoError(t, s.Scopes().AssignToKey(ctx, k2.ID, []string{"billing:read"}))

		require.NoError(t, s.Scopes().Delete(ctx, mine.ID))
		held, err := s.Scopes().ListByKey(ctx, k2.ID)
		require.NoError(t, err)
		require.Len(t, held, 1, "the t2 key still holds its own scope")
		require.Equal(t, theirs.ID, held[0].ID)
		require.Equal(t, "t2", held[0].TenantID)
	})
}

// Paging scopes by offset returns every row exactly once. Two tenants share
// every name, so a list across both has ties on name the order must break.
func TestPagingScopesReturnsEveryRowOnce(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Millisecond)
		all, t1 := map[string]bool{}, map[string]bool{}
		for i := range 125 {
			for _, tenant := range []string{"t1", "t2"} {
				sc := &scope.Scope{ID: id.NewScopeID(), TenantID: tenant, AppID: "app1", Name: "s" + strconv.Itoa(i), CreatedAt: now}
				require.NoError(t, s.Scopes().Create(ctx, sc))
				all[sc.ID.String()] = true
				if tenant == "t1" {
					t1[sc.ID.String()] = true
				}
			}
		}

		pageAll := func(tenant string) map[string]int {
			seen := map[string]int{}
			for offset := 0; ; offset += 33 {
				page, err := s.Scopes().List(ctx, &scope.ListFilter{TenantID: tenant, Limit: 33, Offset: offset})
				require.NoError(t, err)
				for _, sc := range page {
					seen[sc.ID.String()]++
				}
				if len(page) < 33 {
					return seen
				}
			}
		}
		for range 5 {
			for tenant, want := range map[string]map[string]bool{"": all, "t1": t1} {
				seen := pageAll(tenant)
				require.Len(t, seen, len(want), "tenant %q", tenant)
				for sid, n := range seen {
					require.True(t, want[sid], "tenant %q", tenant)
					require.Equal(t, 1, n, "scope %s came back %d times", sid, n)
				}
			}
		}
	})
}
