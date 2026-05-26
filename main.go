package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"

	"github.com/faytranevozter/cctv-bot/bot"
	"github.com/faytranevozter/cctv-bot/config"
	"github.com/joho/godotenv"

	tgbot "github.com/go-telegram/bot"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "error", err)
		os.Exit(1)
	}

	handler := bot.New(cfg)

	b, err := tgbot.New(cfg.BotToken,
		tgbot.WithMiddlewares(bot.AuthMiddleware(cfg.AllowedChatIDs)),
		tgbot.WithDefaultHandler(handler.DefaultHandler),
	)
	if err != nil {
		slog.Error("bot creation failed", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	slog.Info("bot starting")
	b.Start(ctx)
	slog.Info("bot stopped")
}
