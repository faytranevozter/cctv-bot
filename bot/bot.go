package bot

import (
	"bytes"
	"context"
	"errors"
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
	cfg   *config.Config
	store *camera.Store
	sema  camera.Semaphore
}

func New(cfg *config.Config, store *camera.Store) *Handler {
	return &Handler{
		cfg:   cfg,
		store: store,
		sema:  camera.NewSemaphore(cfg.MaxConcurrentCaptures),
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
	text := strings.TrimSpace(update.Message.Text)
	user := update.Message.From.Username

	cmd, rest := splitCommand(text)

	switch strings.ToLower(cmd) {
	case "/start":
		h.cmdStart(ctx, b, chatID)
	case "/help":
		h.cmdHelp(ctx, b, chatID)
	case "/cameras":
		h.cmdCameras(ctx, b, chatID)
	case "/mataelang":
		h.cmdMataelang(ctx, b, chatID, user)
	case "/snap":
		h.cmdSnap(ctx, b, chatID, user, rest)
	case "/addcam":
		h.cmdAddCam(ctx, b, chatID, user, rest)
	case "/delcam":
		h.cmdDelCam(ctx, b, chatID, user, rest)
	}
}

// splitCommand returns the command word and the remaining argument string.
func splitCommand(text string) (cmd, rest string) {
	if i := strings.IndexAny(text, " \t"); i >= 0 {
		return text[:i], strings.TrimSpace(text[i+1:])
	}
	return text, ""
}

// parseNameURL splits an /addcam argument into (name, url). The name may be
// wrapped in single or double quotes to allow spaces; otherwise the first
// whitespace separates name from url.
func parseNameURL(s string) (name, url string, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", false
	}
	if s[0] == '"' || s[0] == '\'' {
		quote := s[0]
		end := strings.IndexByte(s[1:], quote)
		if end < 0 {
			return "", "", false
		}
		name = s[1 : 1+end]
		url = strings.TrimSpace(s[1+end+1:])
	} else {
		i := strings.IndexAny(s, " \t")
		if i < 0 {
			return "", "", false
		}
		name = s[:i]
		url = strings.TrimSpace(s[i+1:])
	}
	if name == "" || url == "" {
		return "", "", false
	}
	return name, url, true
}

func (h *Handler) cmdStart(ctx context.Context, b *tgbot.Bot, chatID int64) {
	msg := "🔍 CCTV Monitor Bot\n\n" +
		"Commands:\n" +
		"/mataelang — Capture a frame from the default camera\n" +
		"/snap <name> — Capture from a specific camera\n" +
		"/cameras — List configured cameras\n" +
		"/addcam \"<name>\" <url> — Add a camera\n" +
		"/delcam <name> — Remove a camera\n" +
		"/help — Show this reference"
	b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: msg})
}

func (h *Handler) cmdHelp(ctx context.Context, b *tgbot.Bot, chatID int64) {
	h.cmdStart(ctx, b, chatID)
}

func (h *Handler) cmdCameras(ctx context.Context, b *tgbot.Bot, chatID int64) {
	cams := h.store.List()
	if len(cams) == 0 {
		b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID: chatID,
			Text:   "No cameras configured. Add one with:\n/addcam \"<name>\" <url>",
		})
		return
	}

	var sb strings.Builder
	sb.WriteString("Cameras:\n")
	for i, cam := range cams {
		masked := camera.MaskCredentials(cam.URL)
		fmt.Fprintf(&sb, "\n• %s", cam.Name)
		if i == 0 {
			sb.WriteString(" (default)")
		}
		fmt.Fprintf(&sb, "\n  %s\n", masked)
	}
	b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: sb.String()})
}

func (h *Handler) cmdMataelang(ctx context.Context, b *tgbot.Bot, chatID int64, user string) {
	cam, ok := h.store.Default()
	if !ok {
		b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID: chatID,
			Text:   "No cameras configured. Add one with /addcam.",
		})
		return
	}
	h.captureAndSend(ctx, b, chatID, user, cam)
}

func (h *Handler) cmdSnap(ctx context.Context, b *tgbot.Bot, chatID int64, user, arg string) {
	name := strings.Trim(arg, " \t\"'")
	if name == "" {
		b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID: chatID,
			Text:   "Usage: /snap <camera_name>\nUse /cameras to list available cameras.",
		})
		return
	}
	cam, ok := h.store.Find(name)
	if !ok {
		b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID: chatID,
			Text:   fmt.Sprintf("Unknown camera: %s. Use /cameras to list.", name),
		})
		return
	}
	h.captureAndSend(ctx, b, chatID, user, cam)
}

func (h *Handler) cmdAddCam(ctx context.Context, b *tgbot.Bot, chatID int64, user, arg string) {
	name, url, ok := parseNameURL(arg)
	if !ok {
		b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID: chatID,
			Text:   "Usage: /addcam \"<name>\" <url>\nExample: /addcam \"Kantor Kiri\" rtsp://user:pass@host/stream",
		})
		return
	}

	err := h.store.Add(camera.Camera{Name: name, URL: url})
	switch {
	case errors.Is(err, camera.ErrAlreadyExists):
		b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID: chatID,
			Text:   fmt.Sprintf("Camera %q already exists. Remove it first with /delcam.", name),
		})
		return
	case errors.Is(err, camera.ErrInvalid):
		b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID: chatID,
			Text:   "Camera name and URL are required.",
		})
		return
	case err != nil:
		slog.Error("addcam failed", "command", "addcam", "chat_id", chatID, "username", user, "camera", name, "error", err.Error())
		b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID: chatID,
			Text:   fmt.Sprintf("Failed to add camera: %s", err.Error()),
		})
		return
	}

	slog.Info("command completed", "command", "addcam", "chat_id", chatID, "username", user, "camera", name)
	b.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID: chatID,
		Text:   fmt.Sprintf("Added camera %q.", name),
	})
}

func (h *Handler) cmdDelCam(ctx context.Context, b *tgbot.Bot, chatID int64, user, arg string) {
	name := strings.Trim(arg, " \t\"'")
	if name == "" {
		b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID: chatID,
			Text:   "Usage: /delcam <name>",
		})
		return
	}
	err := h.store.Remove(name)
	switch {
	case errors.Is(err, camera.ErrNotFound):
		b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID: chatID,
			Text:   fmt.Sprintf("Unknown camera: %s. Use /cameras to list.", name),
		})
		return
	case err != nil:
		slog.Error("delcam failed", "command", "delcam", "chat_id", chatID, "username", user, "camera", name, "error", err.Error())
		b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID: chatID,
			Text:   fmt.Sprintf("Failed to remove camera: %s", err.Error()),
		})
		return
	}

	slog.Info("command completed", "command", "delcam", "chat_id", chatID, "username", user, "camera", name)
	b.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID: chatID,
		Text:   fmt.Sprintf("Removed camera %q.", name),
	})
}

func (h *Handler) captureAndSend(ctx context.Context, b *tgbot.Bot, chatID int64, user string, cam camera.Camera) {
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
			Text:   "Terekam",
		})
	}
}
