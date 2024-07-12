package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/ilyakaznacheev/cleanenv"
	_ "github.com/mattn/go-sqlite3"

	"github.com/ysomad/outline-bot/internal/bot"
	"github.com/ysomad/outline-bot/internal/config"
	"github.com/ysomad/outline-bot/internal/outline"
	"github.com/ysomad/outline-bot/internal/slogx"
	"github.com/ysomad/outline-bot/internal/storage"
)

func main() {
	var conf config.Config

	if err := cleanenv.ReadEnv(&conf); err != nil {
		slogx.Fatal(fmt.Sprintf("config not parsed: %s", err.Error()))
	}

	handler := slog.Handler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slogx.ParseLevel(conf.LogLevel),
	}))

	handler = bot.NewSlogMiddleware(handler)

	slog.SetDefault(slog.New(handler))

	slog.Debug("loaded config", "config", conf)

	db, err := sql.Open("sqlite3", "db.db?_foreign_keys=on")
	if err != nil {
		slogx.Fatal(fmt.Sprintf("db not opened: %s", err.Error()))
	}

	if err = db.Ping(); err != nil {
		slogx.Fatal(fmt.Sprintf("ping failed: %s", err.Error()))
	}

	stateLRU := expirable.NewLRU[string, bot.State](100, nil, time.Hour)
	storage := storage.New(db, sq.StatementBuilder.PlaceholderFormat(sq.Question))

	outlineHttpCli := &http.Client{
		Timeout: conf.Outline.HTTPTimeout,

		// coz my outline without tls :clown:
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}

	outlineClient, err := outline.NewClient(conf.Outline.URL, outline.WithClient(outlineHttpCli))
	if err != nil {
		slogx.Fatal(err.Error())
	}

	bot, err := bot.New(conf.TG, stateLRU, outlineClient, storage)
	if err != nil {
		slogx.Fatal(fmt.Sprintf("bot not initialized: %s", err.Error()))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go bot.NotifyExpiringOrders(ctx, conf.Worker.NotifyExpiringInterval)
	go bot.DeactivateExpiredKeys(ctx, conf.Worker.DeactivateExpiredInterval)
	go bot.Start()

	slog.Info("bot started")
	<-stop
	slog.Info("shutting down")

	cancel()
	slog.Info("stopping bot")
	bot.Stop()
	slog.Info("bot stopped")
}
