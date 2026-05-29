package bot

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/faytranevozter/cctv-bot/auth"
	"github.com/faytranevozter/cctv-bot/camera"
	"github.com/faytranevozter/cctv-bot/config"
	"github.com/faytranevozter/cctv-bot/database"
	"github.com/go-telegram/bot/models"
)

func newHandler(t *testing.T) (*Handler, *camera.Store, *auth.Store, *sql.DB) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })
	cameraStore := camera.NewStore(db)
	authStore, err := auth.NewStore(db, nil)
	if err != nil {
		t.Fatalf("auth.NewStore() error = %v", err)
	}
	cfg := &config.Config{
		SuperuserIDs:          map[int64]bool{1: true},
		FFmpegBin:             "ffmpeg",
		FFmpegTimeoutSec:      15,
		MaxConcurrentCaptures: 3,
		Location:              time.UTC,
	}
	return New(cfg, cameraStore, authStore), cameraStore, authStore, db
}

func TestSplitCommand(t *testing.T) {
	cmd, rest := splitCommand("/snap  Front Gate ")
	if cmd != "/snap" || rest != "Front Gate" {
		t.Fatalf("splitCommand() = %q, %q; want /snap, Front Gate", cmd, rest)
	}
	cmd, rest = splitCommand("/help")
	if cmd != "/help" || rest != "" {
		t.Fatalf("splitCommand() = %q, %q; want /help, empty", cmd, rest)
	}
}

func TestParseNameURL(t *testing.T) {
	tests := []struct {
		input  string
		name   string
		url    string
		wantOK bool
	}{
		{input: `"Front Gate" rtsp://host/stream`, name: "Front Gate", url: "rtsp://host/stream", wantOK: true},
		{input: `'Back Yard' https://host/live.m3u8`, name: "Back Yard", url: "https://host/live.m3u8", wantOK: true},
		{input: `Front rtsp://host/stream`, name: "Front", url: "rtsp://host/stream", wantOK: true},
		{input: `Front`, wantOK: false},
		{input: `"Front rtsp://host/stream`, wantOK: false},
		{input: `"" rtsp://host/stream`, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			name, url, ok := parseNameURL(tt.input)
			if name != tt.name || url != tt.url || ok != tt.wantOK {
				t.Fatalf("parseNameURL(%q) = %q, %q, %v; want %q, %q, %v", tt.input, name, url, ok, tt.name, tt.url, tt.wantOK)
			}
		})
	}
}

func TestShortcutNormalizationValidationAndReservation(t *testing.T) {
	if got := normalizeShortcut(" /Front-Gate 01!! "); got != "front_gate_01" {
		t.Fatalf("normalizeShortcut() = %q, want front_gate_01", got)
	}
	if got := normalizeShortcut("---CAM   01---"); got != "cam_01" {
		t.Fatalf("normalizeShortcut() = %q, want cam_01", got)
	}
	for _, shortcut := range []string{"a", "front_gate", strings.Repeat("a", 32)} {
		if !validShortcut(shortcut) {
			t.Fatalf("validShortcut(%q) = false, want true", shortcut)
		}
	}
	for _, shortcut := range []string{"", "Front", "front-gate", strings.Repeat("a", 33)} {
		if validShortcut(shortcut) {
			t.Fatalf("validShortcut(%q) = true, want false", shortcut)
		}
	}
	for _, shortcut := range []string{"help", "snap", "start", "addcam", "delshortcut"} {
		if !reservedCommand(shortcut) {
			t.Fatalf("reservedCommand(%q) = false, want true", shortcut)
		}
	}
	if reservedCommand("front_gate") {
		t.Fatalf("reservedCommand(front_gate) = true, want false")
	}
}

func TestNormalizeCommandWithBotUsername(t *testing.T) {
	h, _, _, _ := newHandler(t)
	h.SetBotUsername("@CCTipsiBot")

	if cmd, ok := h.normalizeCommand("/Cameras@cctipsibot"); cmd != "/cameras" || !ok {
		t.Fatalf("normalizeCommand() = %q, %v; want /cameras true", cmd, ok)
	}
	if cmd, ok := h.normalizeCommand("/cameras@otherbot"); cmd != "" || ok {
		t.Fatalf("normalizeCommand() = %q, %v; want empty false", cmd, ok)
	}
	if cmd, ok := h.normalizeCommand("/HELP"); cmd != "/help" || !ok {
		t.Fatalf("normalizeCommand() = %q, %v; want /help true", cmd, ok)
	}
}

func TestCommandsIncludeBuiltinsAndCameraShortcuts(t *testing.T) {
	h, store, _, _ := newHandler(t)
	if err := store.Add(camera.Camera{Name: "Front Gate", Shortcut: "front_gate", URL: "rtsp://host/front"}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := store.Add(camera.Camera{Name: "Back", URL: "rtsp://host/back"}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	commands := h.Commands()
	if len(commands) != len(commandHelpItems)+1 {
		t.Fatalf("Commands() len = %d, want %d", len(commands), len(commandHelpItems)+1)
	}
	found := false
	for _, cmd := range commands {
		if cmd.Command == "front_gate" && cmd.Description == "Capture Front Gate" {
			found = true
		}
		if cmd.Command == "Back" || cmd.Command == "back" {
			t.Fatalf("Commands() included camera without shortcut: %#v", cmd)
		}
	}
	if !found {
		t.Fatalf("Commands() = %#v, want front_gate shortcut", commands)
	}
}

func TestAutoShortcut(t *testing.T) {
	h, store, _, _ := newHandler(t)
	shortcut, reason := h.autoShortcut("Front Gate")
	if shortcut != "front_gate" || reason != "" {
		t.Fatalf("autoShortcut() = %q, %q; want front_gate, empty", shortcut, reason)
	}
	shortcut, reason = h.autoShortcut("help")
	if shortcut != "" || !strings.Contains(reason, "reserved") {
		t.Fatalf("autoShortcut(help) = %q, %q; want reserved reason", shortcut, reason)
	}
	shortcut, reason = h.autoShortcut("!!!")
	if shortcut != "" || !strings.Contains(reason, "valid shortcut") {
		t.Fatalf("autoShortcut(!!!) = %q, %q; want invalid reason", shortcut, reason)
	}
	if err := store.Add(camera.Camera{Name: "Existing", Shortcut: "existing", URL: "rtsp://host"}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	shortcut, reason = h.autoShortcut("Existing")
	if shortcut != "" || !strings.Contains(reason, "already used") {
		t.Fatalf("autoShortcut(existing) = %q, %q; want already used reason", shortcut, reason)
	}
}

func TestTextAndKeyboardRendering(t *testing.T) {
	req := auth.Request{ChatID: -100, MessageThreadID: 42, ChatTitle: "Ops", RequestedByID: 7, RequestedByUsername: "operator", Reason: "Need access"}
	text := requestText("New request", req)
	for _, want := range []string{"New request", "Chat: Ops", "Chat ID: -100", "Topic: topic 42", "Requested by: @operator", "User ID: 7", "Reason: Need access"} {
		if !strings.Contains(text, want) {
			t.Fatalf("requestText() = %q, missing %q", text, want)
		}
	}
	keyboard := requestKeyboard(-100, 42)
	if keyboard.InlineKeyboard[0][0].CallbackData != "auth:a:-100:42" || keyboard.InlineKeyboard[0][1].CallbackData != "auth:r:-100:42" {
		t.Fatalf("requestKeyboard() = %#v, want approve/reject callbacks", keyboard)
	}

	authorized := []auth.AuthorizedChat{{ChatID: 10, ChatTitle: "Private"}}
	pending := []auth.Request{{ChatID: 20, MessageThreadID: 12, ChatTitle: "Group", RequestedByID: 9, RequestedByUsername: "admin"}}
	listText := authListText(authorized, pending)
	for _, want := range []string{"Authorized chats:", "1. Private (10)", "Pending requests:", "1. Group (20), topic 12 from @admin"} {
		if !strings.Contains(listText, want) {
			t.Fatalf("authListText() = %q, missing %q", listText, want)
		}
	}
	authKeyboard := authListKeyboard(authorized, pending)
	if len(authKeyboard.InlineKeyboard) != 3 || authKeyboard.InlineKeyboard[2][0].CallbackData != "auth:l" {
		t.Fatalf("authListKeyboard() = %#v, want manage/pending/refresh rows", authKeyboard)
	}
	if authKeyboard.InlineKeyboard[1][0].CallbackData != "auth:a:20:12" || authKeyboard.InlineKeyboard[1][1].CallbackData != "auth:r:20:12" {
		t.Fatalf("authListKeyboard() = %#v, want topic-scoped pending callbacks", authKeyboard)
	}

	cams := []camera.Camera{{ID: 1, Name: "Front Gate", Shortcut: "front_gate", URL: "rtsp://user:pass@host/front"}, {ID: 2, Name: "Back", URL: "rtsp://host/back"}}
	cameraText := cameraListText(cams)
	if !strings.Contains(cameraText, "1. Front Gate (/front_gate)") || !strings.Contains(cameraText, "2. Back") {
		t.Fatalf("cameraListText() = %q, want cameras", cameraText)
	}
	cameraKeyboard := cameraListKeyboard(cams)
	if len(cameraKeyboard.InlineKeyboard) != 4 || cameraKeyboard.InlineKeyboard[1][0].CallbackData != "cam:m:1" {
		t.Fatalf("cameraListKeyboard() = %#v, want add/manage/manage/refresh", cameraKeyboard)
	}
	detailKeyboard := cameraDetailKeyboard(cams[0])
	if len(detailKeyboard.InlineKeyboard) != 6 || detailKeyboard.InlineKeyboard[3][0].CallbackData != "cam:rs:1" {
		t.Fatalf("cameraDetailKeyboard() = %#v, want remove shortcut row", detailKeyboard)
	}
	snapKeyboard := snapPickerKeyboard(cams)
	if len(snapKeyboard.InlineKeyboard) != 2 || snapKeyboard.InlineKeyboard[0][0].CallbackData != "snap:1" {
		t.Fatalf("snapPickerKeyboard() = %#v, want snap callbacks", snapKeyboard)
	}
}

func TestDisplayHelpers(t *testing.T) {
	if got := chatTitle(models.Chat{Title: "Group"}); got != "Group" {
		t.Fatalf("chatTitle(title) = %q", got)
	}
	if got := chatTitle(models.Chat{Username: "user"}); got != "@user" {
		t.Fatalf("chatTitle(username) = %q", got)
	}
	if got := chatTitle(models.Chat{FirstName: "Ada", LastName: "Lovelace"}); got != "Ada Lovelace" {
		t.Fatalf("chatTitle(name) = %q", got)
	}
	if got := chatTitle(models.Chat{ID: 42}); got != "Chat 42" {
		t.Fatalf("chatTitle(fallback) = %q", got)
	}
	if got := displayChat("", 42); got != "Chat 42" {
		t.Fatalf("displayChat() = %q", got)
	}
	if got := displayChatTarget("Group", -100, 12); got != "Group (-100), topic 12" {
		t.Fatalf("displayChatTarget() = %q", got)
	}
	if got := topicLabel(0); got != "general" {
		t.Fatalf("topicLabel(0) = %q", got)
	}
	if got := authorizedAlreadyText(chatTarget{ChatID: -100}); got != "This chat is already authorized." {
		t.Fatalf("authorizedAlreadyText(chat) = %q", got)
	}
	if got := authorizedAlreadyText(chatTarget{ChatID: -100, MessageThreadID: 12}); got != "This topic is already authorized." {
		t.Fatalf("authorizedAlreadyText(topic) = %q", got)
	}
	if got := approvalNotificationText(0); got != "This chat is now authorized." {
		t.Fatalf("approvalNotificationText(chat) = %q", got)
	}
	if got := approvalNotificationText(12); got != "This topic is now authorized." {
		t.Fatalf("approvalNotificationText(topic) = %q", got)
	}
	if got := revokeNotificationText(0); got != "This chat is no longer authorized." {
		t.Fatalf("revokeNotificationText(chat) = %q", got)
	}
	if got := revokeNotificationText(12); got != "This topic is no longer authorized." {
		t.Fatalf("revokeNotificationText(topic) = %q", got)
	}
	if got := displayUser(models.User{ID: 7, Username: "operator"}); got != "@operator" {
		t.Fatalf("displayUser(username) = %q", got)
	}
	if got := displayUser(models.User{ID: 7}); got != "7" {
		t.Fatalf("displayUser(id) = %q", got)
	}
	if got := buttonLabel(strings.Repeat("a", 40)); len(got) != 32 || !strings.HasSuffix(got, "...") {
		t.Fatalf("buttonLabel(long) = %q, want 32-char ellipsis", got)
	}
	target := requestTarget(auth.Request{ChatID: -100, MessageThreadID: 12})
	if target.ChatID != -100 || target.MessageThreadID != 12 {
		t.Fatalf("requestTarget() = %#v, want chat/thread", target)
	}
}
