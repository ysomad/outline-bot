package bot

import (
	"errors"

	"gopkg.in/telebot.v3"
)

func adminMiddleware(admin int64) telebot.MiddlewareFunc {
	return func(next telebot.HandlerFunc) telebot.HandlerFunc {
		return func(c telebot.Context) error {
			if c.Chat().ID != admin {
				return errors.New("access denied")
			}
			return next(c)
		}
	}
}
