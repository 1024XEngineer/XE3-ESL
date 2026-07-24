package requestcontext

import (
	"context"
	"testing"
)

func TestActorRoundTrip(t *testing.T) {
	want := Actor{UserID: "user-1", SessionID: "session-1"}
	got, ok := ActorFromContext(WithActor(context.Background(), want))
	if !ok || got != want {
		t.Fatalf("unexpected actor: %#v, %v", got, ok)
	}
}

func TestActorRejectsIncompleteIdentity(t *testing.T) {
	ctx := WithActor(context.Background(), Actor{UserID: "user-1"})
	if _, ok := ActorFromContext(ctx); ok {
		t.Fatal("expected incomplete actor to be rejected")
	}
}
