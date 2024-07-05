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
	"github.com/ysomad/outline-bot/internal/outline"
	"github.com/ysomad/outline-bot/internal/storage"
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

	now := time.Now()

	slog.Info("callback received", "unique", cb.unique, "data", cb.data, "callback_id", telecb.ID)

	switch step(cb.unique) {
	case stepSelectKeyAmount:
		keyAmount, err := strconv.Atoi(cb.data)
		if err != nil {
			return err
		}

		if err = c.Delete(); err != nil {
			return fmt.Errorf("msg not deleted: %w", err)
		}

		price := keyAmount * domain.PricePerKey

		orderID, err := b.storage.CreateOrder(storage.CreateOrderParams{
			Status:    domain.OrderStatusAwaitingPayment,
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

		if _, err = b.tele.Send(recipient(b.adminID), orderCreatedMsg(orderID, price, keyAmount, usr), adminKb); err != nil {
			return fmt.Errorf("order not sent to admin: %w", err)
		}

		qr := &tele.Photo{
			Caption: fmt.Sprintf("Заказ №%d размещен, к оплате %d₽, оплата по QR коду или кнопке ниже", orderID, price),
			File:    tele.FromDisk(paymentQR),
		}

		return c.Send(qr, paymentKeyboard())
	case stepApproveOrder:
		return b.approveOrder(c, cb)
	case stepRejectOrder:
		orderID, err := domain.OrderIDFromString(cb.data)
		if err != nil {
			return err
		}

		order, err := b.storage.GetOrder(orderID)
		if err != nil {
			return err
		}

		if err := b.storage.Close(orderID, domain.OrderStatusRejected, now); err != nil {
			return fmt.Errorf("order not rejected: %w", err)
		}

		slog.Info("order rejected", "order", order)

		if _, err := b.tele.Send(recipient(order.UID), fmt.Sprintf("Заказ №%d отклонен администратором", orderID)); err != nil {
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

// aproveOrder closes order and creates access keys to outline, sends them to user and admin.
func (b *Bot) approveOrder(c tele.Context, cb btnCallback) error {
	orderID, err := domain.OrderIDFromString(cb.data)
	if err != nil {
		return err
	}

	order, err := b.storage.GetOrder(orderID)
	if err != nil {
		return err
	}

	keys := make([]storage.Key, order.KeyAmount)
	now := time.Now()
	gen := namegenerator.NewNameGenerator(now.UnixNano())
	exp := now.Add(domain.KeyTTL)

	usrSB := &strings.Builder{}
	usrSB.Grow(len(keys) + 1)
	fmt.Fprintf(usrSB, "Заказ №%d одобрен (до %s)\n", order.ID, exp.Format("02.01.2006"))

	for i := range order.KeyAmount {
		keyName := gen.Generate()

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

		keys[i] = storage.Key{
			ID:   key.ID,
			Name: key.Name.Value,
			URL:  key.AccessUrl.Value,
		}
	}

	err = b.storage.ApproveOrder(storage.ApproveOrderParams{
		OID:       orderID,
		Keys:      keys,
		ClosedAt:  now,
		ExpiresAt: exp,
	})
	if err != nil {
		return fmt.Errorf("order not approved: %w", err)
	}

	userMsg := usrSB.String()

	// to write user from order to msg
	usr := user{
		id:        order.UID,
		username:  order.Username.String,
		firstName: order.FirstName.String,
		lastName:  order.LastName.String,
	}

	adminSB := &strings.Builder{}
	adminSB.Grow(5)
	adminSB.WriteString(userMsg)
	adminSB.WriteString("\n")
	usr.write(adminSB)

	if err := c.Edit(adminSB.String(), "", tele.ModeMarkdown); err != nil {
		return fmt.Errorf("order approve msg not sent to admin: %w", err)
	}

	if _, err := b.tele.Send(recipient(order.UID), userMsg, tele.ModeMarkdown); err != nil {
		return fmt.Errorf("keys not sent to user: %w", err)
	}

	return nil
}

func orderCreatedMsg(oid domain.OrderID, price, keys int, usr user) string {
	sb := &strings.Builder{}
	sb.Grow(4)
	fmt.Fprintf(sb, "Новый заказ №%d\n\nК оплате: %d₽\nКлючей: %d\n\n", oid, price, keys)
	usr.write(sb)
	return sb.String()
}
