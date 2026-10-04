package contract

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/usage"

	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"
)

func TestMapError(t *testing.T) {
	cases := []struct {
		err  error
		code dashcontract.ErrorCode
	}{
		{fmt.Errorf("get key: %w", keysmith.ErrKeyNotFound), dashcontract.CodeNotFound},
		{keysmith.ErrPolicyNotFound, dashcontract.CodeNotFound},
		{keysmith.ErrRotationNotFound, dashcontract.CodeNotFound},
		{fmt.Errorf(`scope "x": %w`, keysmith.ErrScopeNotFound), dashcontract.CodeBadRequest},
		{keysmith.ErrScopeNotAllowed, dashcontract.CodeBadRequest},
		{keysmith.ErrKeyLifetimeExceeded, dashcontract.CodeBadRequest},
		{usage.ErrInvalidPeriod, dashcontract.CodeBadRequest},
		{keysmith.ErrInvalidStateTransition, dashcontract.CodeConflict},
		{keysmith.ErrPolicyInUse, dashcontract.CodeConflict},
		{fmt.Errorf("create policy: %w", keysmith.ErrPolicyNameTaken), dashcontract.CodeConflict},
		{fmt.Errorf("create scope: %w", keysmith.ErrScopeNameTaken), dashcontract.CodeConflict},
		{errors.New("pq: connection refused to 10.0.0.5"), dashcontract.CodeInternal},
	}
	for _, tc := range cases {
		mapped := mapError(tc.err)
		assert.Equal(t, tc.code, codeOf(t, mapped), tc.err.Error())
	}
	var ce *dashcontract.Error
	require.ErrorAs(t, mapError(errors.New("pq: connection refused to 10.0.0.5")), &ce)
	assert.NotContains(t, ce.Message, "10.0.0.5", "an internal error's text must not reach the client")
	require.ErrorAs(t, mapError(keysmith.ErrPolicyNameTaken), &ce)
	assert.Equal(t, "a policy with this name already exists", ce.Message)
	require.ErrorAs(t, mapError(keysmith.ErrScopeNameTaken), &ce)
	assert.Equal(t, "a scope with this name already exists", ce.Message)
	assert.Nil(t, mapError(nil))
}
