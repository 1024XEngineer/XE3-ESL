package requestcontext

import "context"

// Actor is the trusted identity derived by server-side authentication.
// Values from request payloads, paths, query strings, or model output must
// never be used to construct an Actor.
type Actor struct {
	UserID    string
	SessionID string
}

func (a Actor) Valid() bool {
	return a.UserID != "" && a.SessionID != ""
}

type actorContextKey struct{}

func WithActor(ctx context.Context, actor Actor) context.Context {
	return context.WithValue(ctx, actorContextKey{}, actor)
}

func ActorFromContext(ctx context.Context) (Actor, bool) {
	actor, ok := ctx.Value(actorContextKey{}).(Actor)
	return actor, ok && actor.Valid()
}
