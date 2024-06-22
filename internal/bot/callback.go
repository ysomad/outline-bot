package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/goombaio/namegenerator"
	tele "gopkg.in/telebot.v3"

	"github.com/ysomad/outline-bot/internal/domain"
	"github.com/ysomad/outline-bot/internal/sqlite"
	"github.com/ysomad/outline-bot/outline"
)

type btnCallback struct {
	unique string
	data   string
}

func parseCallback(data string) (btnCallback, error) {
	data = strings.TrimPrefix(data, "\f")
	dataparts := strings.Split(data, "|")

	switch len(dataparts) {
	case 1:
		return btnCallback{unique: dataparts[0]}, nil
	case 2:
		return btnCallback{unique: dataparts[0], data: dataparts[1]}, nil
	default:
		return btnCallback{}, errors.New("unsupported callback data")
	}
}

func (b *Bot) handleCallback(c tele.Context) error {
	usr := newUser(c.Chat())
	telecb := c.Callback()

	cb, err := parseCallback(telecb.Data)
	if err != nil {
		return err
	}

	now := time.Now().UTC()

	slog.Info("callback received", "unique", cb.unique, "data", cb.data, "callback_id", telecb.ID)

	switch step(cb.unique) {
	case stepSelectKeyAmount:
		keyAmount, err := strconv.Atoi(cb.data)
		if err != nil {
			return err
		}

		if err := c.Delete(); err != nil {
			return fmt.Errorf("msg not deleted: %w", err)
		}

		price := keyAmount * domain.PricePerKey

		orderID, err := b.order.Create(sqlite.CreateOrderParams{
			UID:       usr.id,
			Username:  usr.username,
			FirstName: usr.firstName,
			LastName:  usr.lastName,
			KeyAmount: keyAmount,
			Price:     price,
			CreatedAt: now,
		})
		if err != nil {
			return fmt.Errorf("order not created: %w", err)
		}

		adminKb := &tele.ReplyMarkup{}
		adminKb.Inline(
			adminKb.Row(adminKb.Data("Одобрить", stepApproveOrder.String(), orderID.String())),
			adminKb.Row(adminKb.Data("Отклонить", stepRejectOrder.String(), orderID.String())),
		)

		if _, err = b.Send(recipient(b.adminID), orderCreatedMsg(orderID, price, keyAmount, usr), adminKb); err != nil {
			return fmt.Errorf("order not sent to admin: %w", err)
		}

		usrKb := &tele.ReplyMarkup{}
		usrKb.Inline(usrKb.Row(usrKb.URL("Оплатить", paymentURL)))

		qr := &tele.Photo{
			Caption: fmt.Sprintf("Заказ №%d размещен, к оплате %d₽, оплата по QR коду или кнопке ниже", orderID, price),
			File:    tele.FromDisk(paymentQR),
		}

		return c.Send(qr, usrKb)
	case stepApproveOrder:
		orderID, err := domain.OrderIDFromString(cb.data)
		if err != nil {
			return err
		}

		order, err := b.order.Get(orderID)
		if err != nil {
			return err
		}

		keys := make([]sqlite.AccessKey, order.KeyAmount)
		gen := namegenerator.NewNameGenerator(now.UnixNano())
		exp := now.Add(domain.KeyTTL)

		usrSB := &strings.Builder{}
		usrSB.Grow(len(keys) + 1)
		fmt.Fprintf(usrSB, "Заказ №%d одобрен (до %s)\n", order.ID, exp.Format("02.01.2006"))

		for i := range order.KeyAmount {
			keyName := gen.Generate()

			slog.Debug("creating new key", "name", keyName)

			key, err := b.outline.AccessKeysPost(context.TODO(), outline.NewOptAccessKeysPostReq(outline.AccessKeysPostReq{
				Name: outline.NewOptString(keyName),
			}))
			if err != nil {
				return fmt.Errorf("key not created: %w", err)
			}

			_, err = fmt.Fprintf(usrSB, "\n%s %s\n```\n%s\n```", key.ID, key.Name.Value, key.AccessUrl.Value)
			if err != nil {
				return err
			}

			keys[i] = sqlite.AccessKey{
				ID:        key.ID,
				Name:      key.Name.Value,
				URL:       key.AccessUrl.Value,
				ExpiresAt: exp,
			}
		}

		if err := b.order.Approve(order.ID, keys, now); err != nil {
			return fmt.Errorf("order not approved: %w", err)
		}

		adminSB := &strings.Builder{}
		adminSB.Grow(4)
		adminSB.WriteString(usrSB.String())
		writeUser(adminSB, usr)

		if err := c.Edit(adminSB.String(), "", tele.ModeMarkdown); err != nil {
			return fmt.Errorf("order approve msg not sent to admin: %w", err)
		}

		if _, err := b.Send(recipient(order.UID), usrSB.String(), tele.ModeMarkdown); err != nil {
			return fmt.Errorf("keys not sent to user: %w", err)
		}

		return nil
	case stepRejectOrder:
		orderID, err := domain.OrderIDFromString(cb.data)
		if err != nil {
			return err
		}

		order, err := b.order.Get(orderID)
		if err != nil {
			return err
		}

		if err := b.order.Reject(orderID, now); err != nil {
			return fmt.Errorf("order not rejected: %w", err)
		}

		slog.Info("order rejected", "order", order)

		if _, err := b.Send(recipient(order.UID), fmt.Sprintf("Заказ №%d отклонен администратором", orderID)); err != nil {
			return fmt.Errorf("reject msg not sent to user: %w", err)
		}

		return c.Edit(fmt.Sprintf("Заказ №%d отклонен", orderID))
	case stepCancel:
		if err := c.Delete(); err != nil {
			return err
		}

		b.state.Remove(usr.ID())

		return c.Send("Операция отменена")
	}

	return nil
}

func orderCreatedMsg(oid domain.OrderID, price, keys int, usr user) string {
	sb := &strings.Builder{}
	sb.Grow(4)
	fmt.Fprintf(sb, "Новый заказ №%d\n\nК оплате: %d₽\nКлючей: %d\n", oid, price, keys)
	writeUser(sb, usr)
	return sb.String()
}

func writeUser(sb *strings.Builder, u user) {
	fmt.Fprintf(sb, "\nID: %d", u.id)
	if u.username != "" {
		fmt.Fprintf(sb, "\nЛогин: @%s", u.username)
	}
	if u.firstName != "" {
		fmt.Fprintf(sb, "\nИмя: %s", u.firstName)
	}
	if u.lastName != "" {
		fmt.Fprintf(sb, "\nФамилия: %s", u.lastName)
	}
}
