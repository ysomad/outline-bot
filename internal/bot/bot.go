package bot

import (
	"fmt"

	"github.com/hashicorp/golang-lru/v2/expirable"
	tele "gopkg.in/telebot.v3"
	"gopkg.in/telebot.v3/middleware"

	"github.com/ysomad/outline-bot/internal/outline"
	"github.com/ysomad/outline-bot/internal/storage"
)

const (
	paymentURL = "https://www.tinkoff.ru/rm/malykh.aleksey8/xfHYn24522/"
	paymentQR  = "./assets/qr.jpg"
)

type Bot struct {
	*tele.Bot
	adminID int64
	state   *expirable.LRU[string, State]
	outline *outline.Client
	storage *storage.Storage
}

func New(telebot *tele.Bot, adminID int64, state *expirable.LRU[string, State], outline *outline.Client, st *storage.Storage) (*Bot, error) {
	if err := telebot.SetCommands([]tele.Command{
		{
			Text:        "order",
			Description: "Разместить заказ на оплату",
		},
		{
			Text:        "profile",
			Description: "Узнать статус подписки",
		},
	}); err != nil {
		return nil, fmt.Errorf("commands not set: %w", err)
	}

	telebot.Use(middleware.Recover())

	bot := &Bot{
		Bot:     telebot,
		adminID: adminID,
		storage: st,
		outline: outline,
		state:   state,
	}

	telebot.Handle("/start", bot.handleStart)
	telebot.Handle("/order", bot.handleOrder)
	telebot.Handle("/profile", bot.handleProfile)
	telebot.Handle(tele.OnCallback, bot.handleCallback)

	adminOnly := telebot.Group()
	adminOnly.Use(adminMiddleware(adminID))
	adminOnly.Handle("/admin", bot.handleAdmin)

	return bot, nil
}

func btnCancel(kb *tele.ReplyMarkup) tele.Btn {
	return kb.Data("Отменить", stepCancel.String())
}

func (b *Bot) handleOrder(c tele.Context) error {
	usr := newUser(c.Chat())
	step := stepSelectKeyAmount.String()

	defer b.state.Add(usr.ID(), State{step: step})

	kb := &tele.ReplyMarkup{}
	kb.Inline(
		kb.Row(
			kb.Data("1", step, "1"),
			kb.Data("2", step, "2"),
			kb.Data("3", step, "3"),
			kb.Data("4", step, "4")),
		kb.Row(btnCancel(kb)),
	)

	return c.Send("Сколько ключей доступа к ВПНу хочешь?", kb)
}

func (b *Bot) handleStart(c tele.Context) error {
	msg := "Это бот для доступа к ВПНу ДЛЯ СВОИХ \n\n/order - разместить заказ на доступ к ВПНу\n/profile - статус подписки"
	return c.Send(msg)
}

func (b *Bot) handleProfile(c tele.Context) error {
	/*
	   1. Дата последней оплаты
	   2. Дата следующей оплаты
	   3. Ключ доступа
	*/
	return nil
}

func (b *Bot) handleAdmin(c tele.Context) error {
	// orders, err := b.storage.UnpaidOrders()
	// if err != nil {
	// 	return err
	// }

	// отправить ордера с клавой (Одобрить, отказать)
	// одобрить если пришел платеж и отказать если не пришел
	// for _, o := range orders {
	// 	kb := &tele.ReplyMarkup{}
	// }

	return nil
}
