package bot

import (
	"context"
	"errors"

	tele "gopkg.in/telebot.v3"
)

const ctxKey = "stdcontext"

// stdContext returns context.Context from telebot context.
func stdContext(c tele.Context) context.Context {
	v := c.Get(ctxKey)
	if v == nil {
		return context.Background()
	}
	if ctx, ok := v.(context.Context); ok {
		return ctx
	}
	return context.Background()
}

// contextMiddleware saves user information from tele.Context to context.Context.
func contextMiddleware() tele.MiddlewareFunc {
	return func(next tele.HandlerFunc) tele.HandlerFunc {
		return func(c tele.Context) error {
			ctx := context.Background()
			ch := c.Chat()
			ctx = withUser(ctx, ch.ID, ch.Username)
			c.Set(ctxKey, ctx)
			return next(c)
		}
	}
}

func adminMiddleware(admin int64) tele.MiddlewareFunc {
	return func(next tele.HandlerFunc) tele.HandlerFunc {
		return func(c tele.Context) error {
			if c.Chat().ID != admin {
				return errors.New("access denied")
			}
			return next(c)
		}
	}
}
