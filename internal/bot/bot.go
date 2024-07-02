package bot

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/hashicorp/golang-lru/v2/expirable"
	tele "gopkg.in/telebot.v3"
	"gopkg.in/telebot.v3/middleware"

	"github.com/ysomad/outline-bot/internal/domain"
	"github.com/ysomad/outline-bot/internal/outline"
	"github.com/ysomad/outline-bot/internal/storage"
)

const (
	paymentURL = "https://www.tinkoff.ru/rm/malykh.aleksey8/xfHYn24522/"
	paymentQR  = "./assets/qr.jpg"
)

type Bot struct {
	tele    *tele.Bot
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
		tele:    telebot,
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

func (b *Bot) Start() {
	if b.tele != nil {
		b.tele.Start()
	}
}

func (b *Bot) Stop() {
	if b.tele != nil {
		b.tele.Stop()
	}
}

func btnCancel(kb *tele.ReplyMarkup) tele.Btn {
	return kb.Data("Отменить", stepCancel.String())
}

func (b *Bot) handleOrder(c tele.Context) error {
	usr := newUser(c.Chat())

	keys, err := b.storage.CountActiveKeys(usr.id)
	if err != nil {
		return err
	}

	if keys >= domain.MaxKeysPerUser && usr.id != b.adminID {
		return c.Send("У тебя уже слишком много ключей дружище, гуляй...")
	}

	step := stepSelectKeyAmount.String()

	defer b.state.Add(usr.ID(), State{step: step})

	kb := &tele.ReplyMarkup{}
	kb.Inline(
		kb.Row(
			kb.Data("1", step, "1"),
			kb.Data("2", step, "2"),
			kb.Data("3", step, "3")),
		kb.Row(btnCancel(kb)),
	)

	return c.Send("Сколько ключей доступа к ВПНу хочешь?", kb)
}

func (b *Bot) handleStart(c tele.Context) error {
	msg := "Это бот для доступа к ВПНу ДЛЯ СВОИХ \n\n/order - разместить заказ на доступ к ВПНу\n/profile - статус подписки\n\nКлиент ВПНа можно скачать тут - https://getoutline.org"
	return c.Send(msg)
}

func (b *Bot) handleProfile(c tele.Context) error {
	keys, err := b.storage.ListActiveUserKeys(c.Chat().ID)
	if err != nil {
		return err
	}

	if len(keys) == 0 {
		return c.Send("У тебя нет активных ключей, используй /order для заказа")
	}

	var (
		groupedKeys = make(map[domain.OrderID][]storage.KeyFromOrder)
		oids        []domain.OrderID
	)

	// group keys by order id
	for _, k := range keys {
		oid := k.OrderID

		if _, ok := groupedKeys[oid]; !ok {
			oids = append(oids, oid)
		}

		groupedKeys[oid] = append(groupedKeys[oid], k)
	}

	sb := &strings.Builder{}
	sb.Grow(domain.MaxKeysPerUser + len(oids))

	// build message
	for _, oid := range oids {
		titlePrinted := false

		for _, k := range groupedKeys[oid] {
			// print order title only once
			if !titlePrinted {
				_, err := fmt.Fprintf(sb,
					"\n\n\nЗаказ №%d\nДействует до %s\nСтоимость продления %d руб.\n",
					k.OrderID, k.ExpiresAt.Format("02.01.2006"), k.Price)
				if err != nil {
					return err
				}

				titlePrinted = true
			}

			_, err = fmt.Fprintf(sb, "\n%s %s```%s```", k.ID, k.Name, k.URL)
			if err != nil {
				return err
			}
		}
	}

	slog.Debug(sb.String())

	return c.Send(sb.String(), tele.ModeMarkdown)
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
