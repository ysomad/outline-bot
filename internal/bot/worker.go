package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ysomad/outline-bot/internal/domain"
	"github.com/ysomad/outline-bot/internal/outline"
	"github.com/ysomad/outline-bot/internal/storage"
	tele "gopkg.in/telebot.v3"
)

func startWorker(ctx context.Context, interval time.Duration, f func() error, name string) {
	slog.Info("starting worker", "worker", name)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("worker stopped", "worker", name)
			return
		case <-ticker.C:
			if err := f(); err != nil {
				slog.Error("worker failed", "cause", err.Error(), "worker", name)
			}
		}
	}
}

func (b *Bot) NotifyExpiringOrders(ctx context.Context, interval time.Duration) {
	startWorker(ctx, interval, b.notifyExpiringOrders, "expiring_orders_notifier")
}

func (b *Bot) DeactivateExpiredKeys(ctx context.Context, interval time.Duration) {
	startWorker(ctx, interval, b.deactivateExpiredKeys, "expired_keys_deactivator")
}

func groupExpiringKeys(keys []storage.ExpiringKey) map[order][]storage.ExpiringKey {
	res := make(map[order][]storage.ExpiringKey)

	for _, k := range keys {
		o := order{
			id: k.OrderID,
			user: user{
				id:        k.UID,
				username:  k.Username.String,
				firstName: k.FirstName.String,
				lastName:  k.LastName.String,
			},
			keyAmount: k.KeyAmount,
			price:     k.Price,
			expiresAt: k.ExpiresAt,
		}

		res[o] = append(res[o], k)
	}

	return res
}

type order struct {
	id        domain.OrderID
	user      user
	price     int
	keyAmount int
	expiresAt time.Time
}

func (b *Bot) notifyExpiringOrders() error {
	keys, err := b.storage.ListExpiringKeys(domain.BeforeOrderExpiration)
	if err != nil {
		return fmt.Errorf("expiring keys not listed: %w", err)
	}

	if len(keys) == 0 {
		return nil
	}

	slog.Info("found expiring keys", "amount", len(keys), "keys", keys)

	groupedKeys := groupExpiringKeys(keys)

	sb := &strings.Builder{}

	for order, keys := range groupedKeys {
		_, err := fmt.Fprintf(sb,
			"Ключи по заказу №%d будут деактивированы %s\n\nК оплате %d руб.\nКлючей %d шт.\n\nКлючи к деактивации: ",
			order.id, order.expiresAt.Format("02.01.2006"), order.price, order.keyAmount)
		if err != nil {
			return fmt.Errorf("msg title not written: %w", err)
		}

		for i, k := range keys {
			fmt.Fprintf(sb, "%s %s", k.ID, k.Name)

			if i != len(keys)-1 {
				sb.WriteString(", ")
			}
		}

		qr := &tele.Photo{
			Caption: sb.String(),
			File:    tele.FromDisk(paymentQR),
		}

		slog.Debug("user to sent expiring order", "user", order.user)

		// send to user
		if _, err = b.tele.Send(recipient(order.user.id), qr, paymentKeyboard()); err != nil {
			return fmt.Errorf("not sent to user: %w", err)
		}

		slog.Info("expiring order sent to user", "user_id", order.user.id, "order_id", order.id)

		sb.Reset()

		fmt.Fprintf(sb, "Заказ №%d истекает %s\n\nК оплате %d руб.\nКлючей %d шт.\n\n", order.id, order.expiresAt.Format("02.01.2006"), order.price, order.keyAmount)
		order.user.write(sb)

		kb := &tele.ReplyMarkup{}
		kb.Inline(
			kb.Row(kb.Data("Продлить на 1 месяц", stepOrderRenewApproved.String(), order.id.String())),
			kb.Row(kb.Data("Отклонить продление", stepRejectOrderRenewal.String(), order.id.String())),
		)

		// send to admin
		if _, err = b.tele.Send(recipient(b.adminID), sb.String(), kb); err != nil {
			return fmt.Errorf("renewal msg not sent to admin: %w", err)
		}

		slog.Info("expiring order sent to admin", "order_id", order.id)
	}

	return nil
}

func (b *Bot) deactivateExpiredKeys() error {
	keys, err := b.storage.ListExpiringKeys(0)
	if err != nil {
		return fmt.Errorf("expired keys not listed: %w", err)
	}

	if len(keys) == 0 {
		return nil
	}

	slog.Info("found expired keys", "amount", len(keys))

	groupedKeys := groupExpiringKeys(keys)
	sb := &strings.Builder{}

	for order, keys := range groupedKeys {
		oid := order.id

		// query db in a for loop ;-)
		err := b.storage.CloseOrder(order.id, domain.OrderStatusExpired, time.Now())
		if err != nil {
			return fmt.Errorf("order %d not closed on expiration: %w", oid, err)
		}

		slog.Info("order expired", "oid", oid)

		fmt.Fprintf(sb, "Заказ №%d истек, деактивированы ключи %d шт. на сумму %d руб.\n\n", oid, order.keyAmount, order.price)

		for i, k := range keys {
			_, err := b.outline.AccessKeysIDDelete(context.Background(), outline.AccessKeysIDDeleteParams{ID: k.ID})
			if err != nil {
				return fmt.Errorf("key with id %s not deleted from outline: %w", k.ID, err)
			}

			fmt.Fprintf(sb, "%s %s", k.ID, k.Name)

			if i != len(keys)-1 {
				sb.WriteString(", ")
			}
		}

		if _, err := b.tele.Send(&order.user, sb.String()); err != nil {
			return fmt.Errorf("expired order msg not sent to user: %w", err)
		}

		sb.WriteString("\n\n")
		order.user.write(sb)

		if _, err := b.tele.Send(recipient(b.adminID), sb.String()); err != nil {
			return fmt.Errorf("expired order not sent to admin: %w", err)
		}

		sb.Reset()
	}

	return nil
}
