package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/faytranevozter/cctv-bot/bot"
	"github.com/faytranevozter/cctv-bot/camera"
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

	store, err := camera.OpenStore(cfg.CamerasFile)
	if err != nil {
		slog.Error("cameras store open failed", "path", cfg.CamerasFile, "error", err)
		os.Exit(1)
	}

	// One-shot migration: if the JSON store is empty but legacy CAMERA_N_*
	// env vars are present, seed the file from them. Subsequent edits go
	// through the store and the env vars are ignored.
	if store.Count() == 0 && len(cfg.LegacyCameras) > 0 {
		if err := store.Replace(cfg.LegacyCameras); err != nil {
			slog.Error("legacy camera migration failed", "error", err)
			os.Exit(1)
		}
		slog.Info("migrated cameras from env to file",
			"path", store.Path(),
			"count", len(cfg.LegacyCameras),
		)
	}

	if store.Count() == 0 {
		slog.Warn("no cameras configured; use /addcam in Telegram or edit the file",
			"path", store.Path(),
		)
	}

	handler := bot.New(cfg, store)

	b, err := tgbot.New(cfg.BotToken,
		tgbot.WithMiddlewares(bot.AuthMiddleware(cfg.AllowedChatIDs)),
		tgbot.WithDefaultHandler(handler.DefaultHandler),
	)
	if err != nil {
		slog.Error("bot creation failed", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if _, err := b.SetMyCommands(ctx, &tgbot.SetMyCommandsParams{Commands: bot.Commands()}); err != nil {
		slog.Warn("bot command registration failed", "error", err)
	}

	slog.Info("bot starting", "cameras_file", store.Path(), "cameras", store.Count())
	b.Start(ctx)
	slog.Info("bot stopped")
}
