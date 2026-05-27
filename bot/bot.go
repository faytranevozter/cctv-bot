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
	cfg         *config.Config
	store       *camera.Store
	sema        camera.Semaphore
	botUsername string
}

type commandHelp struct {
	Command     string
	Description string
	Usage       string
}

var commandHelpItems = []commandHelp{
	{Command: "snap", Description: "Capture from a specific camera", Usage: "/snap <name>"},
	{Command: "cameras", Description: "List configured cameras"},
	{Command: "addcam", Description: "Add a camera", Usage: "/addcam \"<name>\" <url>"},
	{Command: "delcam", Description: "Remove a camera", Usage: "/delcam <name>"},
	{Command: "setshortcut", Description: "Assign a camera shortcut", Usage: "/setshortcut \"<name>\" <shortcut>"},
	{Command: "delshortcut", Description: "Remove a camera shortcut", Usage: "/delshortcut <name>"},
	{Command: "help", Description: "Show command reference"},
}

func (h *Handler) Commands() []models.BotCommand {
	commands := make([]models.BotCommand, 0, len(commandHelpItems))
	for _, item := range commandHelpItems {
		commands = append(commands, models.BotCommand{
			Command:     item.Command,
			Description: item.Description,
		})
	}
	for _, cam := range h.store.List() {
		if cam.Shortcut == "" {
			continue
		}
		commands = append(commands, models.BotCommand{
			Command:     cam.Shortcut,
			Description: fmt.Sprintf("Capture %s", cam.Name),
		})
	}
	return commands
}

func (h *Handler) RegisterCommands(ctx context.Context, b *tgbot.Bot) {
	if _, err := b.SetMyCommands(ctx, &tgbot.SetMyCommandsParams{Commands: h.Commands()}); err != nil {
		slog.Warn("bot command registration failed", "error", err)
	}
}

func New(cfg *config.Config, store *camera.Store) *Handler {
	return &Handler{
		cfg:   cfg,
		store: store,
		sema:  camera.NewSemaphore(cfg.MaxConcurrentCaptures),
	}
}

func (h *Handler) SetBotUsername(username string) {
	h.botUsername = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(username), "@"))
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
	chatType := update.Message.Chat.Type
	text := strings.TrimSpace(update.Message.Text)
	user := update.Message.From.Username
	userID := update.Message.From.ID

	cmd, rest := splitCommand(text)
	var ok bool
	cmd, ok = h.normalizeCommand(cmd)
	if !ok {
		return
	}

	switch cmd {
	case "/start":
		h.cmdStart(ctx, b, chatID)
	case "/help":
		h.cmdHelp(ctx, b, chatID)
	case "/cameras":
		h.cmdCameras(ctx, b, chatID)
	case "/snap":
		h.cmdSnap(ctx, b, chatID, user, rest)
	case "/addcam":
		if !h.requireAdmin(ctx, b, chatID, chatType, userID, user, cmd) {
			return
		}
		h.cmdAddCam(ctx, b, chatID, user, rest)
	case "/delcam":
		if !h.requireAdmin(ctx, b, chatID, chatType, userID, user, cmd) {
			return
		}
		h.cmdDelCam(ctx, b, chatID, user, rest)
	case "/setshortcut":
		if !h.requireAdmin(ctx, b, chatID, chatType, userID, user, cmd) {
			return
		}
		h.cmdSetShortcut(ctx, b, chatID, rest)
	case "/delshortcut":
		if !h.requireAdmin(ctx, b, chatID, chatType, userID, user, cmd) {
			return
		}
		h.cmdDelShortcut(ctx, b, chatID, rest)
	default:
		shortcut := strings.TrimPrefix(cmd, "/")
		if cam, ok := h.store.FindByShortcut(shortcut); ok {
			h.captureAndSend(ctx, b, chatID, user, cam)
		}
	}
}

func (h *Handler) normalizeCommand(cmd string) (string, bool) {
	cmd = strings.ToLower(strings.TrimSpace(cmd))
	name, target, hasTarget := strings.Cut(cmd, "@")
	if !hasTarget {
		return name, true
	}
	if h.botUsername == "" || target != h.botUsername {
		return "", false
	}
	return name, true
}

func (h *Handler) requireAdmin(ctx context.Context, b *tgbot.Bot, chatID int64, chatType models.ChatType, userID int64, username, cmd string) bool {
	if chatType == models.ChatTypePrivate {
		return true
	}

	if chatType != models.ChatTypeGroup && chatType != models.ChatTypeSupergroup {
		h.denyAdminCommand(ctx, b, chatID, userID, username, cmd, "unsupported_chat_type")
		return false
	}

	member, err := b.GetChatMember(ctx, &tgbot.GetChatMemberParams{
		ChatID: chatID,
		UserID: userID,
	})
	if err != nil {
		slog.Warn("admin check failed", "chat_id", chatID, "user_id", userID, "username", username, "command", cmd, "error", err.Error())
		b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: "Could not verify admin status. Try again later."})
		return false
	}

	if member.Type == models.ChatMemberTypeOwner || member.Type == models.ChatMemberTypeAdministrator {
		return true
	}

	h.denyAdminCommand(ctx, b, chatID, userID, username, cmd, string(member.Type))
	return false
}

func (h *Handler) denyAdminCommand(ctx context.Context, b *tgbot.Bot, chatID, userID int64, username, cmd, reason string) {
	slog.Warn("admin command denied", "chat_id", chatID, "user_id", userID, "username", username, "command", cmd, "reason", reason)
	b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: "Only group admins can manage cameras and shortcuts."})
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

func parseNameValue(s string) (name, value string, ok bool) {
	name, value, ok = parseNameURL(s)
	return name, value, ok
}

func normalizeShortcut(s string) string {
	s = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "/")
	var sb strings.Builder
	lastUnderscore := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			sb.WriteRune(r)
			lastUnderscore = false
		case r == '_' || r == '-' || r == ' ' || r == '\t':
			if !lastUnderscore && sb.Len() > 0 {
				sb.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	return strings.Trim(sb.String(), "_")
}

func validShortcut(shortcut string) bool {
	if len(shortcut) < 1 || len(shortcut) > 32 {
		return false
	}
	for _, r := range shortcut {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func reservedCommand(shortcut string) bool {
	for _, item := range commandHelpItems {
		if item.Command == shortcut {
			return true
		}
	}
	return shortcut == "start"
}

func (h *Handler) autoShortcut(name string) (string, string) {
	shortcut := normalizeShortcut(name)
	switch {
	case !validShortcut(shortcut):
		return "", "camera name cannot be converted into a valid shortcut"
	case reservedCommand(shortcut):
		return "", fmt.Sprintf("/%s is reserved", shortcut)
	}
	if _, ok := h.store.FindByShortcut(shortcut); ok {
		return "", fmt.Sprintf("/%s is already used", shortcut)
	}
	return shortcut, ""
}

func (h *Handler) cmdStart(ctx context.Context, b *tgbot.Bot, chatID int64) {
	var sb strings.Builder
	sb.WriteString("CCTV Monitor Bot\n\nCommands:\n")
	for i, item := range commandHelpItems {
		if i > 0 {
			sb.WriteByte('\n')
		}
		usage := item.Usage
		if usage == "" {
			usage = "/" + item.Command
		}
		fmt.Fprintf(&sb, "%s - %s", usage, item.Description)
	}
	msg := sb.String()
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
		if cam.Shortcut != "" {
			fmt.Fprintf(&sb, "\n  Shortcut: /%s", cam.Shortcut)
		}
		fmt.Fprintf(&sb, "\n  %s\n", masked)
	}
	b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: sb.String()})
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

	shortcut, shortcutReason := h.autoShortcut(name)
	err := h.store.Add(camera.Camera{Name: name, Shortcut: shortcut, URL: url})
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
	case errors.Is(err, camera.ErrShortcutTaken):
		b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID: chatID,
			Text:   fmt.Sprintf("Shortcut /%s is already used. Add the camera with a different name or set a shortcut manually.", shortcut),
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

	h.RegisterCommands(ctx, b)

	slog.Info("command completed", "command", "addcam", "chat_id", chatID, "username", user, "camera", name)
	msg := fmt.Sprintf("Added camera %q.", name)
	if shortcut != "" {
		msg += fmt.Sprintf("\nShortcut: /%s", shortcut)
	} else {
		msg += fmt.Sprintf("\nNo shortcut created because %s. Set one manually with:\n/setshortcut \"%s\" <shortcut>", shortcutReason, name)
	}
	b.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID: chatID,
		Text:   msg,
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
	h.RegisterCommands(ctx, b)
	b.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID: chatID,
		Text:   fmt.Sprintf("Removed camera %q.", name),
	})
}

func (h *Handler) cmdSetShortcut(ctx context.Context, b *tgbot.Bot, chatID int64, arg string) {
	name, rawShortcut, ok := parseNameValue(arg)
	if !ok {
		b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID: chatID,
			Text:   "Usage: /setshortcut \"<name>\" <shortcut>\nExample: /setshortcut \"Front Gate\" front_gate",
		})
		return
	}

	shortcut := normalizeShortcut(rawShortcut)
	switch {
	case !validShortcut(shortcut):
		b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: "Shortcut must be 1-32 characters and contain only letters, numbers, or underscores."})
		return
	case reservedCommand(shortcut):
		b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: fmt.Sprintf("Shortcut /%s is reserved.", shortcut)})
		return
	}

	err := h.store.SetShortcut(name, shortcut)
	switch {
	case errors.Is(err, camera.ErrNotFound):
		b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: fmt.Sprintf("Unknown camera: %s. Use /cameras to list.", name)})
		return
	case errors.Is(err, camera.ErrShortcutTaken):
		b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: fmt.Sprintf("Shortcut /%s is already used.", shortcut)})
		return
	case err != nil:
		slog.Error("setshortcut failed", "chat_id", chatID, "camera", name, "shortcut", shortcut, "error", err.Error())
		b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: fmt.Sprintf("Failed to set shortcut: %s", err.Error())})
		return
	}

	h.RegisterCommands(ctx, b)
	b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: fmt.Sprintf("Shortcut for %q is now /%s.", name, shortcut)})
}

func (h *Handler) cmdDelShortcut(ctx context.Context, b *tgbot.Bot, chatID int64, arg string) {
	name := strings.Trim(arg, " \t\"'")
	if name == "" {
		b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: "Usage: /delshortcut <name>"})
		return
	}

	err := h.store.DeleteShortcut(name)
	switch {
	case errors.Is(err, camera.ErrNotFound):
		b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: fmt.Sprintf("Unknown camera: %s. Use /cameras to list.", name)})
		return
	case err != nil:
		slog.Error("delshortcut failed", "chat_id", chatID, "camera", name, "error", err.Error())
		b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: fmt.Sprintf("Failed to remove shortcut: %s", err.Error())})
		return
	}

	h.RegisterCommands(ctx, b)
	b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: fmt.Sprintf("Removed shortcut for %q.", name)})
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
