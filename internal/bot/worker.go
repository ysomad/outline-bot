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

func (n *Bot) notifyExpiringOrders() error {
	keys, err := n.storage.ListExpiringKeys(domain.BeforeOrderExpiration)
	if err != nil {
		return fmt.Errorf("expiring keys not listed: %w", err)
	}

	type order struct {
		id        domain.OrderID
		uid       int64
		price     int
		expiresAt time.Time
	}

	groupedKeys := make(map[order][]storage.ExpiringKey)

	for _, k := range keys {
		o := order{
			id:        k.OrderID,
			uid:       k.UID,
			price:     k.Price,
			expiresAt: k.ExpiresAt,
		}
		groupedKeys[o] = append(groupedKeys[o], k)
	}

	slog.Info("found expiring keys", "amount", len(keys), "keys", keys)

	sb := &strings.Builder{}

	for order, keys := range groupedKeys {
		sb.Grow(len(keys)*2 + 1)

		_, err := fmt.Fprintf(sb,
			"Ключи по заказу №%d будут деактивированы %s, к оплате (%d руб.)\n\n",
			order.id, order.expiresAt.Format("02.01.2006"), order.price)
		if err != nil {
			return fmt.Errorf("msg title not written: %w", err)
		}

		for i, k := range keys {
			if _, err := fmt.Fprintf(sb, "%s %s", k.ID, k.Name); err != nil {
				return fmt.Errorf("key msg not written: %w", err)
			}

			if i != len(keys)-1 {
				if _, err := sb.WriteString(", "); err != nil {
					return fmt.Errorf(", not writtern: %w", err)
				}
			}
		}

		qr := &tele.Photo{
			Caption: sb.String(),
			File:    tele.FromDisk(paymentQR),
		}

		if _, err := n.tele.Send(recipient(order.uid), qr, paymentKeyboard()); err != nil {
			return fmt.Errorf("not sent to user: %w", err)
		}

		sb.Reset()
	}

	return nil
}
