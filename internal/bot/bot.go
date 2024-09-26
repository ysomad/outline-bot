package bot

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/hashicorp/golang-lru/v2/expirable"
	tele "gopkg.in/telebot.v3"
	"gopkg.in/telebot.v3/middleware"

	"github.com/ysomad/outline-bot/internal/config"
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

func New(conf config.TG, state *expirable.LRU[string, State], outline *outline.Client, storage *storage.Storage) (b *Bot, err error) {
	b = &Bot{
		adminID: conf.Admin,
		storage: storage,
		outline: outline,
		state:   state,
	}

	b.tele, err = tele.NewBot(tele.Settings{
		Token:   conf.Token,
		OnError: b.handleError,
		Client:  &http.Client{Timeout: conf.HTTPTimeout},
		Poller:  &tele.LongPoller{Timeout: conf.PollerTimeout},
		Verbose: conf.Verbose,
	})
	if err != nil {
		return nil, fmt.Errorf("telebot not created: %w", err)
	}

	err = b.tele.SetCommands([]tele.Command{
		{
			Text:        "order",
			Description: "Разместить заказ на оплату",
		},
		{
			Text:        "profile",
			Description: "Узнать статус подписки",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("telebot commands not set: %w", err)
	}

	b.tele.Use(middleware.Recover())
	b.tele.Use(contextMiddleware())

	b.tele.Handle("/start", b.handleStart)
	b.tele.Handle("/order", b.handleOrder)
	b.tele.Handle("/profile", b.handleProfile)
	b.tele.Handle(tele.OnCallback, b.handleCallback)
	b.tele.Handle(tele.OnText, b.handleText)

	adminOnly := b.tele.Group()
	adminOnly.Use(adminMiddleware(b.adminID))
	adminOnly.Handle("/renew", b.handleRenew)
	adminOnly.Handle("/migrate", b.handleMigration)

	return b, nil
}

func (b *Bot) handleError(err error, c tele.Context) {
	slog.ErrorContext(stdContext(c), "unhandled error happen", "cause", err.Error())
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
	b.state.Add(usr.ID(), State{step: step})

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
		groupedKeys = make(map[domain.OrderID][]storage.ActiveKey)
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

	// build message
	for _, oid := range oids {
		titlePrinted := false

		for _, k := range groupedKeys[oid] {
			// print order title only once
			if !titlePrinted {
				fmt.Fprintf(sb, "\n\n\nЗаказ №%d\nДействует до %s\nСтоимость продления %d руб.\n", k.OrderID, k.ExpiresAt.Format("02.01.2006"), k.Price)
				titlePrinted = true
			}

			fmt.Fprintf(sb, "\n%s %s```%s```", k.ID, k.Name, k.URL)
		}
	}

	return c.Send(sb.String(), tele.ModeMarkdown)
}

func (b *Bot) handleRenew(c tele.Context) error {
	args := c.Args()

	if len(args) != 1 {
		return errors.New("/renew - invalid args")
	}

	n, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("atoi: %w", err)
	}

	oid := domain.OrderID(n)

	if err = b.storage.RenewOrder(oid, domain.OrderTTL); err != nil {
		return fmt.Errorf("order not renewed: %w", err)
	}

	order, err := b.storage.GetOrder(oid)
	if err != nil {
		return fmt.Errorf("order not found: %w", err)
	}

	ctx := stdContext(c)
	ctx = withUser(ctx, order.UID, order.Username.String)

	slog.InfoContext(ctx, "order renewed by admin", "order_id", oid)

	sb := &strings.Builder{}

	fmt.Fprintf(sb, "Заказ №%d продлен до %s\n\nКлючей %d шт.\nОплачено %d руб.", order.ID, order.ExpiresAt.Time.Format("02.01.2006"), order.KeyAmount, order.Price)

	if _, err = b.tele.Send(recipient(order.UID), sb.String()); err != nil {
		return fmt.Errorf("renew msg not sent to user: %w", err)
	}

	sb.WriteString("\n\n")

	usr := user{
		id:        order.UID,
		username:  order.Username.String,
		firstName: order.FirstName.String,
		lastName:  order.LastName.String,
	}

	usr.write(sb)

	return c.Send(sb.String())
}

func (b *Bot) handleMigration(c tele.Context) error {
	usr := newUser(c.Chat())
	b.state.Add(usr.ID(), State{step: stepMigrateKeys.String()})
	return c.Send("Отправь мне новый Management API URL из Outline Manager")
}
