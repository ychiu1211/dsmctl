package cli

import (
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/storage"
)

func TestNewlyCreatedPoolID(t *testing.T) {
	before := storage.State{Pools: []storage.Pool{{ID: "reuse_1"}}}
	after := storage.State{Pools: []storage.Pool{{ID: "reuse_1"}, {ID: "reuse_2"}}}
	if got := newlyCreatedPoolID(before, after); got != "reuse_2" {
		t.Fatalf("added pool: got %q, want reuse_2", got)
	}
	if got := newlyCreatedPoolID(storage.State{}, storage.State{Pools: []storage.Pool{{ID: "pool_1"}}}); got != "pool_1" {
		t.Fatalf("fresh NAS: got %q, want pool_1", got)
	}
	if got := newlyCreatedPoolID(before, before); got != "" {
		t.Fatalf("no new pool: got %q, want empty", got)
	}
}
