package bot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/faytranevozter/cctv-bot/auth"
	"github.com/faytranevozter/cctv-bot/camera"
	"github.com/faytranevozter/cctv-bot/config"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type Handler struct {
	cfg         *config.Config
	store       *camera.Store
	authStore   *auth.Store
	sema        camera.Semaphore
	botUsername string
}

type commandHelp struct {
	Command     string
	Description string
	Usage       string
}

var commandHelpItems = []commandHelp{
	{Command: "requestaccess", Description: "Request access to this bot", Usage: "/requestaccess [reason]"},
	{Command: "authorized", Description: "Manage authorized chats"},
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

func New(cfg *config.Config, store *camera.Store, authStore *auth.Store) *Handler {
	return &Handler{
		cfg:       cfg,
		store:     store,
		authStore: authStore,
		sema:      camera.NewSemaphore(cfg.MaxConcurrentCaptures),
	}
}

func (h *Handler) SetBotUsername(username string) {
	h.botUsername = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(username), "@"))
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
	case "/requestaccess":
		h.cmdRequestAccess(ctx, b, update, rest)
	case "/authorized":
		if !h.requireSuperuser(ctx, b, chatID, userID, user) {
			return
		}
		h.cmdAuthorized(ctx, b, chatID, 0)
	case "/cameras":
		if !h.requireAuthorized(ctx, b, chatID, chatType, userID, user, cmd) {
			return
		}
		h.cmdCameras(ctx, b, chatID)
	case "/snap":
		if !h.requireAuthorized(ctx, b, chatID, chatType, userID, user, cmd) {
			return
		}
		h.cmdSnap(ctx, b, chatID, user, rest)
	case "/addcam":
		if !h.requireAuthorized(ctx, b, chatID, chatType, userID, user, cmd) {
			return
		}
		if !h.requireAdmin(ctx, b, chatID, chatType, userID, user, cmd) {
			return
		}
		h.cmdAddCam(ctx, b, chatID, user, rest)
	case "/delcam":
		if !h.requireAuthorized(ctx, b, chatID, chatType, userID, user, cmd) {
			return
		}
		if !h.requireAdmin(ctx, b, chatID, chatType, userID, user, cmd) {
			return
		}
		h.cmdDelCam(ctx, b, chatID, user, rest)
	case "/setshortcut":
		if !h.requireAuthorized(ctx, b, chatID, chatType, userID, user, cmd) {
			return
		}
		if !h.requireAdmin(ctx, b, chatID, chatType, userID, user, cmd) {
			return
		}
		h.cmdSetShortcut(ctx, b, chatID, rest)
	case "/delshortcut":
		if !h.requireAuthorized(ctx, b, chatID, chatType, userID, user, cmd) {
			return
		}
		if !h.requireAdmin(ctx, b, chatID, chatType, userID, user, cmd) {
			return
		}
		h.cmdDelShortcut(ctx, b, chatID, rest)
	default:
		if !h.requireAuthorized(ctx, b, chatID, chatType, userID, user, cmd) {
			return
		}
		shortcut := strings.TrimPrefix(cmd, "/")
		if cam, ok := h.store.FindByShortcut(shortcut); ok {
			h.captureAndSend(ctx, b, chatID, user, cam)
		}
	}
}

func (h *Handler) CallbackHandler(ctx context.Context, b *tgbot.Bot, update *models.Update) {
	if update.CallbackQuery == nil {
		return
	}
	q := update.CallbackQuery
	if !strings.HasPrefix(q.Data, "auth:") {
		return
	}

	if !h.isSuperuser(q.From.ID) {
		b.AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{CallbackQueryID: q.ID, Text: "Only superusers can use this button.", ShowAlert: true})
		return
	}
	b.AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{CallbackQueryID: q.ID})

	parts := strings.Split(q.Data, ":")
	if len(parts) < 2 {
		return
	}
	action := parts[1]
	chatID, messageID, ok := callbackMessage(q)
	if !ok {
		return
	}

	switch action {
	case "a", "r", "m", "v":
		if len(parts) != 3 {
			return
		}
		targetID, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return
		}
		switch action {
		case "a":
			h.approveRequest(ctx, b, chatID, messageID, targetID, q.From)
		case "r":
			h.rejectRequest(ctx, b, chatID, messageID, targetID, q.From)
		case "m":
			h.renderAuthManage(ctx, b, chatID, messageID, targetID)
		case "v":
			h.revokeAuthorized(ctx, b, chatID, messageID, targetID)
		}
	case "l":
		h.cmdAuthorized(ctx, b, chatID, messageID)
	}
}

func callbackMessage(q *models.CallbackQuery) (chatID int64, messageID int, ok bool) {
	if q.Message.Message == nil {
		return 0, 0, false
	}
	return q.Message.Message.Chat.ID, q.Message.Message.ID, true
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

func (h *Handler) isSuperuser(userID int64) bool {
	return h.cfg.SuperuserIDs[userID]
}

func (h *Handler) requireSuperuser(ctx context.Context, b *tgbot.Bot, chatID, userID int64, username string) bool {
	if h.isSuperuser(userID) {
		return true
	}
	slog.Warn("superuser command denied", "chat_id", chatID, "user_id", userID, "username", username)
	b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: "Only superusers can use this command."})
	return false
}

func (h *Handler) requireAuthorized(ctx context.Context, b *tgbot.Bot, chatID int64, chatType models.ChatType, userID int64, username, cmd string) bool {
	if h.authStore.IsAuthorized(chatID) || (chatType == models.ChatTypePrivate && h.isSuperuser(userID)) {
		return true
	}
	slog.Warn("unauthorized", "chat_id", chatID, "user_id", userID, "username", username, "command", cmd)
	b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: "This chat is not authorized. Ask a group admin to run /requestaccess."})
	return false
}

func (h *Handler) isGroupAdmin(ctx context.Context, b *tgbot.Bot, chatID int64, chatType models.ChatType, userID int64) (bool, error) {
	if chatType == models.ChatTypePrivate {
		return true, nil
	}
	if chatType != models.ChatTypeGroup && chatType != models.ChatTypeSupergroup {
		return false, nil
	}
	member, err := b.GetChatMember(ctx, &tgbot.GetChatMemberParams{ChatID: chatID, UserID: userID})
	if err != nil {
		return false, err
	}
	return member.Type == models.ChatMemberTypeOwner || member.Type == models.ChatMemberTypeAdministrator, nil
}

func (h *Handler) requireAdmin(ctx context.Context, b *tgbot.Bot, chatID int64, chatType models.ChatType, userID int64, username, cmd string) bool {
	ok, err := h.isGroupAdmin(ctx, b, chatID, chatType, userID)
	if err != nil {
		slog.Warn("admin check failed", "chat_id", chatID, "user_id", userID, "username", username, "command", cmd, "error", err.Error())
		b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: "Could not verify admin status. Try again later."})
		return false
	}
	if ok {
		return true
	}
	h.denyAdminCommand(ctx, b, chatID, userID, username, cmd, "not_admin")
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

func (h *Handler) cmdRequestAccess(ctx context.Context, b *tgbot.Bot, update *models.Update, reason string) {
	msg := update.Message
	chatID := msg.Chat.ID
	chatType := msg.Chat.Type
	userID := msg.From.ID
	username := msg.From.Username

	if h.authStore.IsAuthorized(chatID) || (chatType == models.ChatTypePrivate && h.isSuperuser(userID)) {
		b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: "This chat is already authorized."})
		return
	}

	if chatType == models.ChatTypeGroup || chatType == models.ChatTypeSupergroup {
		ok, err := h.isGroupAdmin(ctx, b, chatID, chatType, userID)
		if err != nil {
			slog.Warn("request access admin check failed", "chat_id", chatID, "user_id", userID, "username", username, "error", err.Error())
			b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: "Could not verify admin status. Try again later."})
			return
		}
		if !ok {
			b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: "Only group admins can request access for this group."})
			return
		}
	}

	if _, ok := h.authStore.Pending(chatID); ok {
		b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: "Access request is already pending."})
		return
	}

	req := auth.Request{
		ChatID:              chatID,
		ChatType:            string(chatType),
		ChatTitle:           chatTitle(msg.Chat),
		RequestedByID:       userID,
		RequestedByUsername: username,
		Reason:              strings.TrimSpace(reason),
		RequestedAt:         time.Now().UTC(),
	}
	if err := h.authStore.UpsertPending(req); err != nil {
		slog.Error("request access failed", "chat_id", chatID, "error", err.Error())
		b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: fmt.Sprintf("Failed to create access request: %s", err.Error())})
		return
	}

	b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: fmt.Sprintf("Access request sent to superuser.\nChat ID: %d", chatID)})
	h.notifySuperusers(ctx, b, req)
}

func chatTitle(chat models.Chat) string {
	if chat.Title != "" {
		return chat.Title
	}
	if chat.Username != "" {
		return "@" + chat.Username
	}
	name := strings.TrimSpace(strings.TrimSpace(chat.FirstName + " " + chat.LastName))
	if name != "" {
		return name
	}
	return fmt.Sprintf("Chat %d", chat.ID)
}

func (h *Handler) notifySuperusers(ctx context.Context, b *tgbot.Bot, req auth.Request) {
	for userID := range h.cfg.SuperuserIDs {
		if _, err := b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID:      userID,
			Text:        requestText("New CCTV bot access request", req),
			ReplyMarkup: requestKeyboard(req.ChatID),
		}); err != nil {
			slog.Warn("superuser notification failed", "superuser_id", userID, "request_chat_id", req.ChatID, "error", err.Error())
		}
	}
}

func requestText(title string, req auth.Request) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s\n\n", title)
	fmt.Fprintf(&sb, "Chat: %s\n", displayChat(req.ChatTitle, req.ChatID))
	fmt.Fprintf(&sb, "Chat ID: %d\n", req.ChatID)
	if req.RequestedByUsername != "" {
		fmt.Fprintf(&sb, "Requested by: @%s\n", req.RequestedByUsername)
	}
	fmt.Fprintf(&sb, "User ID: %d\n", req.RequestedByID)
	if req.Reason != "" {
		fmt.Fprintf(&sb, "Reason: %s\n", req.Reason)
	}
	return strings.TrimSpace(sb.String())
}

func requestKeyboard(chatID int64) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{
		{Text: "Approve", CallbackData: fmt.Sprintf("auth:a:%d", chatID)},
		{Text: "Reject", CallbackData: fmt.Sprintf("auth:r:%d", chatID)},
	}}}
}

func displayChat(title string, chatID int64) string {
	if title != "" {
		return title
	}
	return fmt.Sprintf("Chat %d", chatID)
}

func (h *Handler) cmdAuthorized(ctx context.Context, b *tgbot.Bot, chatID int64, messageID int) {
	authorized := h.authStore.ListAuthorized()
	pending := h.authStore.ListPending()
	text := authListText(authorized, pending)
	markup := authListKeyboard(authorized, pending)
	if messageID > 0 {
		b.EditMessageText(ctx, &tgbot.EditMessageTextParams{ChatID: chatID, MessageID: messageID, Text: text, ReplyMarkup: markup})
		return
	}
	b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: text, ReplyMarkup: markup})
}

func authListText(authorized []auth.AuthorizedChat, pending []auth.Request) string {
	var sb strings.Builder
	sb.WriteString("Authorized chats:\n")
	if len(authorized) == 0 {
		sb.WriteString("none\n")
	} else {
		for i, chat := range authorized {
			fmt.Fprintf(&sb, "%d. %s (%d)\n", i+1, displayChat(chat.ChatTitle, chat.ChatID), chat.ChatID)
		}
	}
	sb.WriteString("\nPending requests:\n")
	if len(pending) == 0 {
		sb.WriteString("none")
	} else {
		for i, req := range pending {
			by := fmt.Sprintf("%d", req.RequestedByID)
			if req.RequestedByUsername != "" {
				by = "@" + req.RequestedByUsername
			}
			fmt.Fprintf(&sb, "%d. %s (%d) from %s\n", i+1, displayChat(req.ChatTitle, req.ChatID), req.ChatID, by)
		}
	}
	return strings.TrimSpace(sb.String())
}

func authListKeyboard(authorized []auth.AuthorizedChat, pending []auth.Request) *models.InlineKeyboardMarkup {
	var rows [][]models.InlineKeyboardButton
	for _, chat := range authorized {
		rows = append(rows, []models.InlineKeyboardButton{{
			Text:         "Manage: " + buttonLabel(displayChat(chat.ChatTitle, chat.ChatID)),
			CallbackData: fmt.Sprintf("auth:m:%d", chat.ChatID),
		}})
	}
	for _, req := range pending {
		label := buttonLabel(displayChat(req.ChatTitle, req.ChatID))
		rows = append(rows, []models.InlineKeyboardButton{
			{Text: "Approve: " + label, CallbackData: fmt.Sprintf("auth:a:%d", req.ChatID)},
			{Text: "Reject", CallbackData: fmt.Sprintf("auth:r:%d", req.ChatID)},
		})
	}
	rows = append(rows, []models.InlineKeyboardButton{{Text: "Refresh", CallbackData: "auth:l"}})
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func buttonLabel(s string) string {
	if len(s) <= 32 {
		return s
	}
	return s[:29] + "..."
}

func (h *Handler) approveRequest(ctx context.Context, b *tgbot.Bot, chatID int64, messageID int, targetID int64, user models.User) {
	req, ok, err := h.authStore.RemovePending(targetID)
	if err != nil {
		slog.Error("approve request failed", "chat_id", targetID, "error", err.Error())
		b.EditMessageText(ctx, &tgbot.EditMessageTextParams{ChatID: chatID, MessageID: messageID, Text: fmt.Sprintf("Failed to approve chat %d: %s", targetID, err.Error())})
		return
	}
	if !ok {
		b.EditMessageText(ctx, &tgbot.EditMessageTextParams{ChatID: chatID, MessageID: messageID, Text: fmt.Sprintf("No pending request for chat %d.", targetID)})
		return
	}
	approved := auth.AuthorizedChat{ChatID: req.ChatID, ChatType: req.ChatType, ChatTitle: req.ChatTitle, ApprovedByID: user.ID, ApprovedByUsername: user.Username, ApprovedAt: time.Now().UTC()}
	if err := h.authStore.AddAuthorized(approved); err != nil {
		slog.Error("save approval failed", "chat_id", targetID, "error", err.Error())
		b.EditMessageText(ctx, &tgbot.EditMessageTextParams{ChatID: chatID, MessageID: messageID, Text: fmt.Sprintf("Failed to save approval for chat %d: %s", targetID, err.Error())})
		return
	}
	b.EditMessageText(ctx, &tgbot.EditMessageTextParams{ChatID: chatID, MessageID: messageID, Text: fmt.Sprintf("Approved CCTV bot access request\n\nChat: %s\nChat ID: %d\nApproved by: %s", displayChat(req.ChatTitle, req.ChatID), req.ChatID, displayUser(user))})
	if _, err := b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: req.ChatID, Text: "This chat is now authorized."}); err != nil {
		slog.Warn("approval notification failed", "chat_id", req.ChatID, "error", err.Error())
	}
}

func (h *Handler) rejectRequest(ctx context.Context, b *tgbot.Bot, chatID int64, messageID int, targetID int64, user models.User) {
	req, ok, err := h.authStore.RemovePending(targetID)
	if err != nil {
		slog.Error("reject request failed", "chat_id", targetID, "error", err.Error())
		b.EditMessageText(ctx, &tgbot.EditMessageTextParams{ChatID: chatID, MessageID: messageID, Text: fmt.Sprintf("Failed to reject chat %d: %s", targetID, err.Error())})
		return
	}
	if !ok {
		b.EditMessageText(ctx, &tgbot.EditMessageTextParams{ChatID: chatID, MessageID: messageID, Text: fmt.Sprintf("No pending request for chat %d.", targetID)})
		return
	}
	b.EditMessageText(ctx, &tgbot.EditMessageTextParams{ChatID: chatID, MessageID: messageID, Text: fmt.Sprintf("Rejected CCTV bot access request\n\nChat: %s\nChat ID: %d\nRejected by: %s", displayChat(req.ChatTitle, req.ChatID), req.ChatID, displayUser(user))})
	if _, err := b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: req.ChatID, Text: "Access request was rejected."}); err != nil {
		slog.Warn("rejection notification failed", "chat_id", req.ChatID, "error", err.Error())
	}
}

func (h *Handler) renderAuthManage(ctx context.Context, b *tgbot.Bot, chatID int64, messageID int, targetID int64) {
	var target auth.AuthorizedChat
	found := false
	for _, chat := range h.authStore.ListAuthorized() {
		if chat.ChatID == targetID {
			target = chat
			found = true
			break
		}
	}
	if !found {
		b.EditMessageText(ctx, &tgbot.EditMessageTextParams{ChatID: chatID, MessageID: messageID, Text: fmt.Sprintf("Authorized chat %d was not found.", targetID), ReplyMarkup: backKeyboard()})
		return
	}
	text := fmt.Sprintf("Authorized chat\n\nChat: %s\nChat ID: %d", displayChat(target.ChatTitle, target.ChatID), target.ChatID)
	if target.ApprovedByUsername != "" {
		text += fmt.Sprintf("\nApproved by: @%s", target.ApprovedByUsername)
	}
	if !target.ApprovedAt.IsZero() {
		text += fmt.Sprintf("\nApproved at: %s", target.ApprovedAt.Format(time.RFC3339))
	}
	b.EditMessageText(ctx, &tgbot.EditMessageTextParams{ChatID: chatID, MessageID: messageID, Text: text, ReplyMarkup: revokeKeyboard(target.ChatID)})
}

func (h *Handler) revokeAuthorized(ctx context.Context, b *tgbot.Bot, chatID int64, messageID int, targetID int64) {
	if err := h.authStore.RemoveAuthorized(targetID); err != nil {
		slog.Error("revoke failed", "chat_id", targetID, "error", err.Error())
		b.EditMessageText(ctx, &tgbot.EditMessageTextParams{ChatID: chatID, MessageID: messageID, Text: fmt.Sprintf("Failed to revoke chat %d: %s", targetID, err.Error())})
		return
	}
	b.EditMessageText(ctx, &tgbot.EditMessageTextParams{ChatID: chatID, MessageID: messageID, Text: fmt.Sprintf("Revoked access\n\nChat ID: %d", targetID), ReplyMarkup: backKeyboard()})
	if _, err := b.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: targetID, Text: "This chat is no longer authorized."}); err != nil {
		slog.Warn("revoke notification failed", "chat_id", targetID, "error", err.Error())
	}
}

func revokeKeyboard(chatID int64) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		{{Text: "Revoke Access", CallbackData: fmt.Sprintf("auth:v:%d", chatID)}},
		{{Text: "Back to List", CallbackData: "auth:l"}},
	}}
}

func backKeyboard() *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{{Text: "Back to List", CallbackData: "auth:l"}}}}
}

func displayUser(user models.User) string {
	if user.Username != "" {
		return "@" + user.Username
	}
	return fmt.Sprintf("%d", user.ID)
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
