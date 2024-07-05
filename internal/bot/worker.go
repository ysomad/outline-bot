package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ysomad/outline-bot/internal/domain"
	"github.com/ysomad/outline-bot/internal/storage"
	tele "gopkg.in/telebot.v3"
)

// StartWorker starts worker which is running jobs in parallel every b.workerInterval.
func (b *Bot) StartWorker(ctx context.Context) {
	ticker := time.NewTicker(b.workerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("scheduler stopped")
			return
		case <-ticker.C:
			slog.Info("RUNNING WORKER POGGERS")
			if err := b.notifyExpiringOrders(); err != nil {
				slog.Error("expiring orders not notified", "cause", err.Error())
			}
		}
	}
}

func (b *Bot) notifyExpiringOrders() error {
	keys, err := b.storage.ListExpiringKeys(domain.BeforeOrderExpiration)
	if err != nil {
		return fmt.Errorf("expiring keys not listed: %w", err)
	}

	type order struct {
		id        domain.OrderID
		user      user
		price     int
		keyAmount int
		expiresAt time.Time
	}

	groupedKeys := make(map[order][]storage.ExpiringKey)

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
		groupedKeys[o] = append(groupedKeys[o], k)
	}

	slog.Info("found expiring keys", "amount", len(keys), "keys", keys)

	sb := &strings.Builder{}

	for order, keys := range groupedKeys {
		_, err := fmt.Fprintf(sb,
			"Ключи по заказу №%d будут деактивированы %s\n\nК оплате %d руб.\nКлючей %d шт.\n\nКлючи к деактивации: ",
			order.id, order.expiresAt.Format("02.01.2006"), order.price, order.keyAmount)
		if err != nil {
			return fmt.Errorf("msg title not written: %w", err)
		}

		for i, k := range keys {
			if _, err = fmt.Fprintf(sb, "%s %s", k.ID, k.Name); err != nil {
				return fmt.Errorf("key msg not written: %w", err)
			}

			if i != len(keys)-1 {
				if _, err = sb.WriteString(", "); err != nil {
					return fmt.Errorf(", not writtern: %w", err)
				}
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

		sb.Reset()

		if _, err = fmt.Fprintf(sb,
			"Заказ №%d истекает %s\n\nК оплате %d руб.\nКлючей %d шт.\n\n",
			order.id, order.expiresAt.Format("02.01.2006"), order.price, order.keyAmount); err != nil {
			return fmt.Errorf("order title not written: %w", err)
		}

		if err = order.user.write(sb); err != nil {
			return fmt.Errorf("user not written to builder: %w", err)
		}

		kb := &tele.ReplyMarkup{}
		kb.Inline(
			kb.Row(kb.Data("Продлить на 1 месяц", stepRenewOrder.String(), order.id.String())),
			kb.Row(kb.Data("Отклонить продление", stepRejectOrderRenewal.String(), order.id.String())),
		)

		// send to admin
		if _, err = b.tele.Send(recipient(b.adminID), sb.String(), kb); err != nil {
			return fmt.Errorf("renewal msg not sent to admin: %w", err)
		}
	}

	return nil
}
