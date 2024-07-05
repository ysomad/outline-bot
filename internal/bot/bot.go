package bot

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

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
	tele           *tele.Bot
	adminID        int64
	workerInterval time.Duration
	state          *expirable.LRU[string, State]
	outline        *outline.Client
	storage        *storage.Storage
}

type Params struct {
	Telebot        *tele.Bot
	AdminID        int64
	State          *expirable.LRU[string, State]
	Outline        *outline.Client
	Storage        *storage.Storage
	WorkerInterval time.Duration
}

func New(p *Params) (*Bot, error) {
	if err := p.Telebot.SetCommands([]tele.Command{
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

	p.Telebot.Use(middleware.Recover())

	b := &Bot{
		tele:           p.Telebot,
		adminID:        p.AdminID,
		storage:        p.Storage,
		outline:        p.Outline,
		state:          p.State,
		workerInterval: p.WorkerInterval,
	}

	b.tele.Handle("/start", b.handleStart)
	b.tele.Handle("/order", b.handleOrder)
	b.tele.Handle("/profile", b.handleProfile)
	b.tele.Handle(tele.OnCallback, b.handleCallback)

	adminOnly := b.tele.Group()
	adminOnly.Use(adminMiddleware(b.adminID))
	adminOnly.Handle("/renew", b.handleRenew)

	return b, nil
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
	sb.Grow(domain.MaxKeysPerUser + len(oids))

	// build message
	for _, oid := range oids {
		titlePrinted := false

		for _, k := range groupedKeys[oid] {
			// print order title only once
			if !titlePrinted {
				_, err = fmt.Fprintf(sb,
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

	o, err := b.storage.GetOrder(oid)
	if err != nil {
		return fmt.Errorf("order not found: %w", err)
	}

	slog.Info("order renewed by admin", "oid", n)

	sb := &strings.Builder{}

	_, err = fmt.Fprintf(sb, "Заказ №%d продлен до %s\n\nКлючей %d шт.\nОплачено %d руб.",
		o.ID, o.ExpiresAt.Time.Format("02.01.2006"), o.KeyAmount, o.Price)
	if err != nil {
		return fmt.Errorf("renew msg not written: %w", err)
	}

	if _, err = b.tele.Send(recipient(o.UID), sb.String()); err != nil {
		return fmt.Errorf("renew msg not sent to user: %w", err)
	}

	if _, err = sb.WriteString("\n\n"); err != nil {
		return fmt.Errorf("\n not written: %w", err)
	}

	usr := user{
		id:        o.UID,
		username:  o.Username.String,
		firstName: o.FirstName.String,
		lastName:  o.LastName.String,
	}

	if err = usr.write(sb); err != nil {
		return fmt.Errorf("usr not written: %w", err)
	}

	return c.Send(sb.String())
}
