package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/faytranevozter/cctv-bot/auth"
	"github.com/faytranevozter/cctv-bot/bot"
	"github.com/faytranevozter/cctv-bot/camera"
	"github.com/faytranevozter/cctv-bot/config"
	"github.com/faytranevozter/cctv-bot/database"
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

	db, err := database.Open(cfg.DBFile)
	if err != nil {
		slog.Error("database open failed", "path", cfg.DBFile, "error", err)
		os.Exit(1)
	}
	defer db.Close()

	store := camera.NewStore(db)

	// One-shot migration: if the database has no cameras but legacy CAMERA_N_*
	// env vars are present, seed the database from them.
	if store.Count() == 0 && len(cfg.LegacyCameras) > 0 {
		if err := store.Replace(cfg.LegacyCameras); err != nil {
			slog.Error("legacy camera migration failed", "error", err)
			os.Exit(1)
		}
		slog.Info("migrated cameras from env to database",
			"path", cfg.DBFile,
			"count", len(cfg.LegacyCameras),
		)
	}

	if store.Count() == 0 {
		slog.Warn("no cameras configured; use /addcam in Telegram",
			"path", cfg.DBFile,
		)
	}

	authStore, err := auth.NewStore(db, cfg.AuthorizedChatIDs)
	if err != nil {
		slog.Error("auth store open failed", "path", cfg.DBFile, "error", err)
		os.Exit(1)
	}

	handler := bot.New(cfg, store, authStore)

	b, err := tgbot.New(cfg.BotToken,
		tgbot.WithDefaultHandler(handler.DefaultHandler),
		tgbot.WithCallbackQueryDataHandler("auth:", tgbot.MatchTypePrefix, handler.CallbackHandler),
	)
	if err != nil {
		slog.Error("bot creation failed", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	me, err := b.GetMe(ctx)
	if err != nil {
		slog.Warn("bot username lookup failed", "error", err)
	} else {
		handler.SetBotUsername(me.Username)
	}

	handler.RegisterCommands(ctx, b)

	slog.Info("bot starting", "db_file", cfg.DBFile, "cameras", store.Count())
	b.Start(ctx)
	slog.Info("bot stopped")
}
