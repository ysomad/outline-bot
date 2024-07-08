package bot

import (
	"context"
	"log/slog"

	"github.com/ysomad/outline-bot/internal/domain"
)

type logCtxKey struct{}

type logCtx struct {
	UID            int64
	Username       string
	OrderID        domain.OrderID
	CallbackUnique string
	CallbackData   string
}

func withUser(ctx context.Context, uid int64, username string) context.Context {
	if c, ok := ctx.Value(logCtxKey{}).(logCtx); ok {
		c.UID = uid
		c.Username = username
		return context.WithValue(ctx, logCtxKey{}, c)
	}

	return context.WithValue(ctx, logCtxKey{}, logCtx{
		UID:      uid,
		Username: username,
	})
}

func withOrderID(ctx context.Context, oid domain.OrderID) context.Context {
	if c, ok := ctx.Value(logCtxKey{}).(logCtx); ok {
		c.OrderID = oid
		return context.WithValue(ctx, logCtxKey{}, c)
	}

	return context.WithValue(ctx, logCtxKey{}, logCtx{OrderID: oid})
}

func withCallback(ctx context.Context, cb btnCallback) context.Context {
	if c, ok := ctx.Value(logCtxKey{}).(logCtx); ok {
		c.CallbackData = cb.data
		c.CallbackUnique = cb.unique
		return context.WithValue(ctx, logCtxKey{}, c)
	}

	return context.WithValue(ctx, logCtxKey{}, logCtx{
		CallbackUnique: cb.unique,
		CallbackData:   cb.data,
	})
}

var _ slog.Handler = &slogMiddleware{}

type slogMiddleware struct {
	next slog.Handler
}

func NewSlogMiddleware(next slog.Handler) *slogMiddleware {
	return &slogMiddleware{next: next}
}

func (m *slogMiddleware) Enabled(ctx context.Context, l slog.Level) bool {
	return m.next.Enabled(ctx, l)
}

func (m *slogMiddleware) WithAttrs(attrs []slog.Attr) slog.Handler {
	return m.next.WithAttrs(attrs)
}

func (m *slogMiddleware) WithGroup(name string) slog.Handler {
	return m.next.WithGroup(name)
}

func (m *slogMiddleware) Handle(ctx context.Context, req slog.Record) error {
	if c, ok := ctx.Value(logCtxKey{}).(logCtx); ok {
		if c.UID != 0 {
			req.Add("user_id", c.UID)
		}
		if c.Username != "" {
			req.Add("username", c.Username)
		}
		if c.OrderID != 0 {
			req.Add("order_id", c.OrderID)
		}
		if c.CallbackData != "" {
			req.Add("callback_data", c.CallbackData)
		}
		if c.CallbackUnique != "" {
			req.Add("callback_unique", c.CallbackUnique)
		}
	}
	return m.next.Handle(ctx, req)
}
