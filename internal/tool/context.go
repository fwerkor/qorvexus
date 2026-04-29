package tool

import (
	"context"

	"qorvexus/internal/types"
)

type conversationContextKey struct{}
type sessionIDKey struct{}
type subAgentDepthKey struct{}

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

func WithSubAgentDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, subAgentDepthKey{}, depth)
}

func SubAgentDepthFrom(ctx context.Context) int {
	depth, ok := ctx.Value(subAgentDepthKey{}).(int)
	if !ok || depth < 0 {
		return 0
	}
	return depth
}
