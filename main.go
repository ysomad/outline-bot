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
	"strings"
	"syscall"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/ilyakaznacheev/cleanenv"
	_ "github.com/mattn/go-sqlite3"
	tele "gopkg.in/telebot.v3"

	"github.com/ysomad/outline-bot/internal/bot"
	"github.com/ysomad/outline-bot/internal/config"
	"github.com/ysomad/outline-bot/internal/outline"
	"github.com/ysomad/outline-bot/internal/storage"
)

func slogFatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}

func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error", "err":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func main() {
	var conf config.Config

	if err := cleanenv.ReadEnv(&conf); err != nil {
		slogFatal(fmt.Sprintf("config not parsed: %s", err.Error()))
	}

	handler := slog.Handler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(conf.LogLevel),
	}))

	slog.SetDefault(slog.New(handler))

	slog.Debug("loaded config", "config", conf)

	db, err := sql.Open("sqlite3", "db.db")
	if err != nil {
		slogFatal(fmt.Sprintf("db not opened: %s", err.Error()))
	}

	if err = db.Ping(); err != nil {
		slogFatal(fmt.Sprintf("ping failed: %s", err.Error()))
	}

	stateLRU := expirable.NewLRU[string, bot.State](100, nil, time.Hour*24) // TODO: move ttl to config
	storage := storage.New(db, sq.StatementBuilder.PlaceholderFormat(sq.Question))

	outlineHttpCli := &http.Client{
		Timeout:   time.Second * 3,                                                         // TODO: move timeout to config
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, // coz my outline without tls :clown:
	}

	outlineCli, err := outline.NewClient(conf.OutlineURL, outline.WithClient(outlineHttpCli))
	if err != nil {
		slogFatal(err.Error())
	}

	telebotHttpCli := &http.Client{
		Timeout: time.Second * 10, // TODO: move timeout to config
	}

	telebot, err := tele.NewBot(tele.Settings{
		Token: conf.TGToken,
		OnError: func(err error, c tele.Context) {
			slog.Error(fmt.Sprintf("unhandled error: %s", err.Error()))
		},
		Client:  telebotHttpCli,
		Poller:  &tele.LongPoller{Timeout: time.Second * 3}, // TODO: move timeout to config
		Verbose: false,
	})
	if err != nil {
		slogFatal(fmt.Sprintf("telebot not created: %s", err.Error()))
	}

	bot, err := bot.New(&bot.Params{
		Telebot: telebot,
		AdminID: conf.TGAdmin,
		State:   stateLRU,
		Outline: outlineCli,
		Storage: storage,
	})
	if err != nil {
		slogFatal(fmt.Sprintf("bot not initialized: %s", err.Error()))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go bot.NotifyExpiringOrders(ctx, time.Second*30) // TODO: move interval to config
	go bot.DeactivateExpiredKeys(ctx, time.Hour)     // TODO: move interval to config
	go bot.Start()

	slog.Info("bot started")
	<-stop
	slog.Info("shutting down")

	cancel()
	slog.Info("stopping bot")
	bot.Stop()
	slog.Info("bot stopped")
}
