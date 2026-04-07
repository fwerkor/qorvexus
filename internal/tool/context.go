package tool

import (
	"context"

	"qorvexus/internal/types"
)

type conversationContextKey struct{}

func WithConversationContext(ctx context.Context, convo types.ConversationContext) context.Context {
	return context.WithValue(ctx, conversationContextKey{}, convo)
}

func ConversationContextFrom(ctx context.Context) (types.ConversationContext, bool) {
	convo, ok := ctx.Value(conversationContextKey{}).(types.ConversationContext)
	return convo, ok
}
