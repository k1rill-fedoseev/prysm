package slasher

import (
	"context"
	"testing"

	"github.com/prysmaticlabs/prysm/shared/event"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
)

func TestService_StartStop(t *testing.T) {
	srv, err := New(context.Background(), &ServiceConfig{
		IndexedAttsFeed: new(event.Feed),
	})
	require.NoError(t, err)
	go srv.Start()
	require.NoError(t, srv.Stop())
	require.NoError(t, srv.Status())
}