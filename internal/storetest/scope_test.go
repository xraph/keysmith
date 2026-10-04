package storetest_test

import (
	"context"
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
