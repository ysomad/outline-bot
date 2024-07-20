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
	ctx := stdContext(c)

	cb, err := parseCallback(telecb.Data)
	if err != nil {
		return err
	}

	ctx = withCallback(ctx, cb)
	now := time.Now()

	slog.InfoContext(ctx, "callback received")

	switch step(cb.unique) {
	case stepSelectKeyAmount:
		return b.selectKeyAmount(c, ctx, cb, usr, now)
	case stepApproveOrder:
		return b.approveOrder(c, ctx, cb)
	case stepOrderRenewApproved:
		return b.renewOrder(c, ctx, cb)
	case stepRejectOrder, stepRejectOrderRenewal:
		return b.rejectOrder(c, ctx, cb, now)
	case stepCancel:
		if err := c.Delete(); err != nil {
			return fmt.Errorf("step cancel: %w", err)
		}
		b.state.Remove(usr.ID())
		return c.Send("Операция отменена")
	default:
		return fmt.Errorf("unsupported callback: %s", cb.unique)
	}
}

// selectKeyAmount triggers after user selected amount of keys to create.
// Creates order, sends payment details to the user and to admin which have to approve or reject the order.
func (b *Bot) selectKeyAmount(c tele.Context, ctx context.Context, cb btnCallback, usr *user, now time.Time) error {
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

	ctx = withOrderID(ctx, orderID)
	slog.InfoContext(ctx, "order created by user")

	adminKb := &tele.ReplyMarkup{}
	adminKb.Inline(
		adminKb.Row(adminKb.Data("Одобрить", stepApproveOrder.String(), orderID.String())),
		adminKb.Row(adminKb.Data("Отклонить", stepRejectOrder.String(), orderID.String())),
	)

	_, err = b.tele.Send(recipient(b.adminID), orderCreatedMsg(orderID, price, keyAmount, usr), adminKb)
	if err != nil {
		return fmt.Errorf("order not sent to admin: %w", err)
	}

	qr := &tele.Photo{
		Caption: fmt.Sprintf("Заказ №%d размещен, к оплате %d₽, оплата по QR коду или кнопке ниже. После оплаты админ одобрит заказ и я пришлю тебе ключи доступа к ВПНу", orderID, price),
		File:    tele.FromDisk(paymentQR),
	}

	return c.Send(qr, paymentKeyboard())
}

// renewOrder triggers when order renew approved.
// Receives order id from callback, sets new expiration timestamp and returns it to user and admin.
func (b *Bot) renewOrder(c tele.Context, ctx context.Context, cb btnCallback) error {
	orderID, err := domain.OrderIDFromString(cb.data)
	if err != nil {
		return fmt.Errorf("order id not found in callback data: %w", err)
	}

	ctx = withOrderID(ctx, orderID)

	if err = b.storage.RenewOrder(orderID, domain.OrderTTL); err != nil {
		return fmt.Errorf("order not renewed: %w", err)
	}

	slog.InfoContext(ctx, "order renewed", "ttl", domain.OrderTTL)

	order, err := b.storage.GetOrder(orderID)
	if err != nil {
		return fmt.Errorf("order not found: %w", err)
	}

	sb := &strings.Builder{}

	fmt.Fprintf(sb, "Заказ №%d продлен до %s\n\nКлючей %d шт.\nОплачено %d руб.", order.ID, order.ExpiresAt.Time.Format("02.01.2006"), order.KeyAmount, order.Price)

	// send to user
	if _, err = b.tele.Send(recipient(order.UID), sb.String()); err != nil {
		return fmt.Errorf("renew msg not sent to user: %w", err)
	}

	sb.WriteString("\n\n")

	orderUser := user{
		id:        order.UID,
		username:  order.Username.String,
		firstName: order.FirstName.String,
		lastName:  order.LastName.String,
	}

	orderUser.write(sb)

	// sent to admin
	return c.Edit(sb.String())
}

// rejectOrder trigger when admin rejects order renew operation.
// Closes the order and set status "rejected".
func (b *Bot) rejectOrder(c tele.Context, ctx context.Context, cb btnCallback, now time.Time) error {
	orderID, err := domain.OrderIDFromString(cb.data)
	if err != nil {
		return fmt.Errorf("order id not found in callback data on reject: %w", err)
	}

	ctx = withOrderID(ctx, orderID)

	order, err := b.storage.GetOrder(orderID)
	if err != nil {
		return fmt.Errorf("order not found on reject: %w", err)
	}

	if err = b.storage.CloseOrder(orderID, domain.OrderStatusRejected, now); err != nil {
		return fmt.Errorf("order not closed on reject: %w", err)
	}

	slog.InfoContext(ctx, "order rejected by admin")

	sb := &strings.Builder{}

	fmt.Fprintf(sb, "Заказ №%d на сумму %d руб. отклонен", order.ID, order.Price)

	orderUser := &user{
		id:        order.UID,
		username:  order.Username.String,
		firstName: order.FirstName.String,
		lastName:  order.LastName.String,
	}

	if _, err = b.tele.Send(orderUser, sb.String()); err != nil {
		return fmt.Errorf("reject msg not sent to user: %w", err)
	}

	sb.WriteString("\n\n")
	orderUser.write(sb)

	return c.Edit(sb.String())
}

// aproveOrder approved order and creates access keys to outline, sends them to user and admin.
// Triggers after admin approved order in inline keyboard.
func (b *Bot) approveOrder(c tele.Context, ctx context.Context, cb btnCallback) error {
	orderID, err := domain.OrderIDFromString(cb.data)
	if err != nil {
		return err
	}

	ctx = withOrderID(ctx, orderID)

	order, err := b.storage.GetOrder(orderID)
	if err != nil {
		return err
	}

	ctx = withUser(ctx, order.UID, order.Username.String)

	now := time.Now()
	gen := namegenerator.NewNameGenerator(now.UnixNano())

	keys := make([]storage.Key, order.KeyAmount)
	expiresAt := now.Add(domain.OrderTTL)

	sb := &strings.Builder{}

	fmt.Fprintf(sb, "Заказ №%d одобрен (до %s)\n", orderID, expiresAt.Format("02.01.2006"))

	for i := range order.KeyAmount {
		keyName := gen.Generate()

		key, err := b.outline.AccessKeysPost(ctx, outline.NewOptAccessKeysPostReq(outline.AccessKeysPostReq{
			Name: outline.NewOptString(keyName),
		}))
		if err != nil {
			return fmt.Errorf("outline key not created: %w", err)
		}

		slog.InfoContext(ctx, "created key in outline", "key_id", key.ID, "key_name", key.Name)

		fmt.Fprintf(sb, "\n%s %s\n```\n%s\n```", key.ID, key.Name.Value, key.AccessUrl.Value)

		keys[i] = storage.Key{
			ID:   key.ID,
			Name: key.Name.Value,
			URL:  key.AccessUrl.Value,
		}
	}

	err = b.storage.ApproveOrder(orderID, keys, expiresAt)
	if err != nil {
		return fmt.Errorf("order not approved: %w", err)
	}

	slog.InfoContext(ctx, "order approved by admin")

	// to write user from order to msg
	usr := &user{
		id:        order.UID,
		username:  order.Username.String,
		firstName: order.FirstName.String,
		lastName:  order.LastName.String,
	}

	// send to user
	if _, err := b.tele.Send(usr, sb.String(), tele.ModeMarkdown); err != nil {
		return fmt.Errorf("order approve msg not sent to user: %w", err)
	}

	sb.WriteString("\n")
	usr.write(sb)

	slog.InfoContext(ctx, "msg to admin", "msg", sb.String())

	// send to admin
	if err := c.Edit(sb.String(), "", tele.ModeMarkdown); err != nil {
		return fmt.Errorf("order approve msg not sent to admin: %w", err)
	}

	return nil
}

func orderCreatedMsg(oid domain.OrderID, price, keys int, usr *user) string {
	sb := &strings.Builder{}
	fmt.Fprintf(sb, "Новый заказ №%d\n\nК оплате: %d₽\nКлючей: %d\n\n", oid, price, keys)
	usr.write(sb)
	return sb.String()
}
