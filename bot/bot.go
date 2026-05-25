package bot

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/faytranevozter/cctv-bot/camera"
	"github.com/faytranevozter/cctv-bot/config"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type Handler struct {
	cfg  *config.Config
	sema camera.Semaphore
}

func New(cfg *config.Config) *Handler {
	return &Handler{
		cfg:  cfg,
		sema: camera.NewSemaphore(cfg.MaxConcurrentCaptures),
	}
}

func AuthMiddleware(allowed map[int64]bool) tgbot.Middleware {
	return func(next tgbot.HandlerFunc) tgbot.HandlerFunc {
		return func(ctx context.Context, b *tgbot.Bot, update *models.Update) {
			if update.Message == nil {
				next(ctx, b, update)
				return
			}
			if !allowed[update.Message.Chat.ID] {
				slog.Warn("unauthorized",
					"chat_id", update.Message.Chat.ID,
					"username", update.Message.From.Username,
				)
				return
			}
			next(ctx, b, update)
		}
	}
}

func (h *Handler) DefaultHandler(ctx context.Context, b *tgbot.Bot, update *models.Update) {
	if update.Message == nil || update.Message.Text == "" {
		return
	}

	chatID := update.Message.Chat.ID
	text := update.Message.Text
	user := update.Message.From.Username

	parts := strings.Fields(text)
	if len(parts) == 0 {
		return
	}
	cmd := strings.ToLower(parts[0])

	switch cmd {
	case "/start":
		h.cmdStart(ctx, b, chatID)
	case "/help":
		h.cmdHelp(ctx, b, chatID)
	case "/cameras":
		h.cmdCameras(ctx, b, chatID)
	case "/mataelang":
		h.cmdMataelang(ctx, b, chatID, user)
	case "/snap":
		arg := ""
		if len(parts) > 1 {
			arg = parts[1]
		}
		h.cmdSnap(ctx, b, chatID, user, arg)
	}
}

func (h *Handler) cmdStart(ctx context.Context, b *tgbot.Bot, chatID int64) {
	msg := "🔍 CCTV Monitor Bot\n\n" +
		"Commands:\n" +
		"/mataelang — Capture a frame from the default camera\n" +
		"/snap <name> — Capture from a specific camera\n" +
		"/cameras — List configured cameras\n" +
		"/help — Show this reference"
	b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: msg})
}

func (h *Handler) cmdHelp(ctx context.Context, b *tgbot.Bot, chatID int64) {
	h.cmdStart(ctx, b, chatID)
}

func (h *Handler) cmdCameras(ctx context.Context, b *tgbot.Bot, chatID int64) {
	if len(h.cfg.Cameras) == 0 {
		b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: "No cameras configured."})
		return
	}

	var sb strings.Builder
	sb.WriteString("Cameras:\n")
	for i, cam := range h.cfg.Cameras {
		masked := camera.MaskCredentials(cam.URL)
		sb.WriteString(fmt.Sprintf("\n• %s", cam.Name))
		if i == 0 {
			sb.WriteString(" (default)")
		}
		sb.WriteString(fmt.Sprintf("\n  %s\n", masked))
	}
	b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: sb.String()})
}

func (h *Handler) cmdMataelang(ctx context.Context, b *tgbot.Bot, chatID int64, user string) {
	h.captureAndSend(ctx, b, chatID, user, h.cfg.DefaultCamera())
}

func (h *Handler) cmdSnap(ctx context.Context, b *tgbot.Bot, chatID int64, user, name string) {
	if name == "" {
		b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID: chatID,
			Text:   "Usage: /snap <camera_name>\nUse /cameras to list available cameras.",
		})
		return
	}
	cam, ok := h.cfg.FindCamera(name)
	if !ok {
		b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID: chatID,
			Text:   fmt.Sprintf("Unknown camera: %s. Use /cameras to list.", name),
		})
		return
	}
	h.captureAndSend(ctx, b, chatID, user, cam)
}

func (h *Handler) captureAndSend(ctx context.Context, b *tgbot.Bot, chatID int64, user string, cam config.Camera) {
	start := time.Now()

	b.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID: chatID,
		Text:   "Capturing frame, please wait...",
	})

	h.sema.Acquire()
	defer h.sema.Release()

	path, err := camera.Capture(ctx, cam.URL, h.cfg.FFmpegBin, h.cfg.FFmpegTimeoutSec)
	if err != nil {
		dur := time.Since(start).Milliseconds()
		slog.Error("capture failed",
			"command", "capture",
			"chat_id", chatID,
			"username", user,
			"camera", cam.Name,
			"duration_ms", dur,
			"error", err.Error(),
		)
		b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID: chatID,
			Text:   fmt.Sprintf("Failed to capture frame: %s", err.Error()),
		})
		return
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		dur := time.Since(start).Milliseconds()
		slog.Error("read failed",
			"command", "capture",
			"chat_id", chatID,
			"username", user,
			"camera", cam.Name,
			"duration_ms", dur,
			"error", err.Error(),
		)
		b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID: chatID,
			Text:   fmt.Sprintf("Failed to read frame: %s", err.Error()),
		})
		return
	}

	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		loc = time.UTC
	}
	caption := fmt.Sprintf("%s · %s", cam.Name, time.Now().In(loc).Format("2006-01-02 15:04:05 WIB"))

	_, sendErr := b.SendPhoto(ctx, &tgbot.SendPhotoParams{
		ChatID:  chatID,
		Caption: caption,
		Photo:   &models.InputFileUpload{Filename: "snapshot.jpg", Data: bytes.NewReader(data)},
	})

	dur := time.Since(start).Milliseconds()
	if sendErr != nil {
		slog.Error("send failed",
			"command", "capture",
			"chat_id", chatID,
			"username", user,
			"camera", cam.Name,
			"duration_ms", dur,
			"error", sendErr.Error(),
		)
	} else {
		slog.Info("command completed",
			"command", "capture",
			"chat_id", chatID,
			"username", user,
			"camera", cam.Name,
			"duration_ms", dur,
		)
		b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID: chatID,
			Text:   fmt.Sprintf("Terekam"),
		})
	}
}
