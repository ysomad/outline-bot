package bot

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/goombaio/namegenerator"
	"github.com/ysomad/outline-bot/internal/outline"
	"github.com/ysomad/outline-bot/internal/storage"
	tele "gopkg.in/telebot.v3"
)

func (b *Bot) handleText(c tele.Context) error {
	usr := newUser(c.Chat())

	state, ok := b.state.Get(usr.ID())
	if !ok {
		return errors.New("no state found in text handler")
	}

	switch step(state.step) {
	case stepMigrateKeys:
		outlineURL, err := url.Parse(c.Text())
		if err != nil {
			return c.Send(fmt.Sprintf("url parse (%s): %s", c.Text(), err.Error()))
		}

		outlineHttpCli := &http.Client{
			Timeout:   time.Second * 5,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		}

		// outline client with new url
		outlineClient, err := outline.NewClient(outlineURL.String(), outline.WithClient(outlineHttpCli))
		if err != nil {
			return c.Send("outline new client: " + err.Error())
		}

		orders, err := b.storage.ListActiveOrders()
		if err != nil {
			return c.Send("list active orders: " + err.Error())
		}

		if err := b.storage.DeteleAllKeys(); err != nil {
			return c.Send("delete all keys: %w", err)
		}

		now := time.Now()
		gen := namegenerator.NewNameGenerator(now.UnixNano())
		sb := &strings.Builder{}

		for _, order := range orders {
			keys := make([]storage.Key, order.KeyAmount)

			fmt.Fprintf(sb, "Заказ №%d пересоздан, срок окончания ключей не изменился (до %s)\n", order.ID, order.ExpiresAt.Time.Format("02.01.2006"))

			for i := range order.KeyAmount {
				keyName := gen.Generate()

				newKey, err := outlineClient.AccessKeysPost(context.TODO(), outline.NewOptAccessKeysPostReq(outline.AccessKeysPostReq{
					Name: outline.NewOptString(keyName),
				}))
				if err != nil {
					return fmt.Errorf("outline key not created: %w", err)
				}

				slog.Info("created key in outline", "key_id", newKey.ID, "key_name", newKey.Name.Value)

				fmt.Fprintf(sb, "\n%s %s\n```\n%s\n```", newKey.ID, newKey.Name.Value, newKey.AccessUrl.Value)

				keys[i] = storage.Key{
					ID:   newKey.ID,
					Name: keyName,
					URL:  newKey.AccessUrl.Value,
				}
			}

			fmt.Fprintf(sb, "Старые ключи работать перестанут, не забудь поменять ключи в Outline!")

			if err := b.storage.ApproveOrder(order.ID, keys, order.ExpiresAt.Time); err != nil {
				return fmt.Errorf("order not approved: %w", err)
			}

			if _, err := b.tele.Send(recipient(order.UID), sb.String(), tele.ModeMarkdown); err != nil {
				slog.Error("order approve msg not sent to user: " + err.Error())
			}

			sb.Reset()
		}

		b.state.Remove(usr.ID())

		return c.Send("Все ключи мигрированы, не забудь обновить OUTLINE_URL, OUTLINE_HOST в .envv!!!")
	default:
		return errors.New("unsupported text step")
	}
}
