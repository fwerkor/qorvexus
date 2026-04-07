package tool

import (
	"context"

	"qorvexus/internal/types"
)

type conversationContextKey struct{}
type sessionIDKey struct{}

func WithConversationContext(ctx context.Context, convo types.ConversationContext) context.Context {
	return context.WithValue(ctx, conversationContextKey{}, convo)
}

func ConversationContextFrom(ctx context.Context) (types.ConversationContext, bool) {
	convo, ok := ctx.Value(conversationContextKey{}).(types.ConversationContext)
	return convo, ok
}

func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionIDKey{}, sessionID)
}

func SessionIDFrom(ctx context.Context) (string, bool) {
	sessionID, ok := ctx.Value(sessionIDKey{}).(string)
	return sessionID, ok
}
