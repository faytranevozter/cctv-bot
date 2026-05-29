package camera

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/faytranevozter/cctv-bot/database"
)

func newCameraStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "camera.db"))
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewStore(db), db
}

func TestStoreStartsEmpty(t *testing.T) {
	store, _ := newCameraStore(t)
	if store.Count() != 0 {
		t.Fatalf("Count() = %d, want 0", store.Count())
	}
	if cams := store.List(); len(cams) != 0 {
		t.Fatalf("List() = %#v, want empty", cams)
	}
}

func TestAddListAndFindCameras(t *testing.T) {
	store, _ := newCameraStore(t)
	if err := store.Add(Camera{Name: " Front Gate ", Shortcut: " /front_gate ", URL: " rtsp://u:p@host/stream "}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := store.Add(Camera{Name: "Back", URL: "rtmp://host/live"}); err != nil {
		t.Fatalf("Add() second error = %v", err)
	}

	if store.Count() != 2 {
		t.Fatalf("Count() = %d, want 2", store.Count())
	}
	cams := store.List()
	if len(cams) != 2 || cams[0].Name != "Front Gate" || cams[0].Shortcut != "front_gate" || cams[0].URL != "rtsp://u:p@host/stream" || cams[1].Name != "Back" {
		t.Fatalf("List() = %#v, want trimmed insertion order", cams)
	}
	if cam, ok := store.Find("front gate"); !ok || cam.ID == 0 || cam.Name != "Front Gate" {
		t.Fatalf("Find() = %#v, %v; want Front Gate", cam, ok)
	}
	if cam, ok := store.FindByShortcut("/FRONT_GATE"); !ok || cam.Name != "Front Gate" {
		t.Fatalf("FindByShortcut() = %#v, %v; want Front Gate", cam, ok)
	}
	if cam, ok := store.FindByID(cams[1].ID); !ok || cam.Name != "Back" {
		t.Fatalf("FindByID() = %#v, %v; want Back", cam, ok)
	}
}

func TestAddRejectsInvalidAndDuplicates(t *testing.T) {
	store, _ := newCameraStore(t)
	if err := store.Add(Camera{Name: "", URL: "rtsp://host"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Add() empty name = %v, want ErrInvalid", err)
	}
	if err := store.Add(Camera{Name: "Front", Shortcut: "front", URL: "rtsp://host"}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := store.Add(Camera{Name: "front", URL: "rtsp://other"}); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("Add() duplicate name = %v, want ErrAlreadyExists", err)
	}
	if err := store.Add(Camera{Name: "Other", Shortcut: "FRONT", URL: "rtsp://other"}); !errors.Is(err, ErrShortcutTaken) {
		t.Fatalf("Add() duplicate shortcut = %v, want ErrShortcutTaken", err)
	}
}

func TestRenameShortcutAndRemoveLifecycle(t *testing.T) {
	store, _ := newCameraStore(t)
	if err := store.Add(Camera{Name: "Front", Shortcut: "front", URL: "rtsp://front"}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := store.Add(Camera{Name: "Back", Shortcut: "back", URL: "rtsp://back"}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	front, _ := store.Find("Front")
	back, _ := store.Find("Back")

	if err := store.RenameByID(front.ID, " Gate "); err != nil {
		t.Fatalf("RenameByID() error = %v", err)
	}
	if cam, ok := store.Find("gate"); !ok || cam.Name != "Gate" {
		t.Fatalf("Find renamed camera = %#v, %v; want Gate", cam, ok)
	}
	if err := store.RenameByID(front.ID, "Back"); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("RenameByID() duplicate = %v, want ErrAlreadyExists", err)
	}
	if err := store.RenameByID(999, "Missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RenameByID() missing = %v, want ErrNotFound", err)
	}
	if err := store.RenameByID(front.ID, " "); !errors.Is(err, ErrInvalid) {
		t.Fatalf("RenameByID() invalid = %v, want ErrInvalid", err)
	}

	if err := store.SetShortcut("Gate", "/gate"); err != nil {
		t.Fatalf("SetShortcut() error = %v", err)
	}
	if err := store.SetShortcut("Gate", "back"); !errors.Is(err, ErrShortcutTaken) {
		t.Fatalf("SetShortcut() taken = %v, want ErrShortcutTaken", err)
	}
	if err := store.SetShortcut("Missing", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetShortcut() missing = %v, want ErrNotFound", err)
	}
	if err := store.SetShortcut("Gate", ""); !errors.Is(err, ErrInvalidShortcut) {
		t.Fatalf("SetShortcut() invalid = %v, want ErrInvalidShortcut", err)
	}

	if err := store.SetShortcutByID(front.ID, "gate2"); err != nil {
		t.Fatalf("SetShortcutByID() error = %v", err)
	}
	if err := store.SetShortcutByID(front.ID, "back"); !errors.Is(err, ErrShortcutTaken) {
		t.Fatalf("SetShortcutByID() taken = %v, want ErrShortcutTaken", err)
	}
	if err := store.SetShortcutByID(999, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetShortcutByID() missing = %v, want ErrNotFound", err)
	}
	if err := store.SetShortcutByID(0, "zero"); !errors.Is(err, ErrInvalidShortcut) {
		t.Fatalf("SetShortcutByID() invalid = %v, want ErrInvalidShortcut", err)
	}

	if err := store.DeleteShortcut("Gate"); err != nil {
		t.Fatalf("DeleteShortcut() error = %v", err)
	}
	if cam, _ := store.FindByID(front.ID); cam.Shortcut != "" {
		t.Fatalf("Shortcut after DeleteShortcut() = %q, want empty", cam.Shortcut)
	}
	if err := store.DeleteShortcut("Missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteShortcut() missing = %v, want ErrNotFound", err)
	}
	if err := store.DeleteShortcutByID(back.ID); err != nil {
		t.Fatalf("DeleteShortcutByID() error = %v", err)
	}
	if err := store.DeleteShortcutByID(0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteShortcutByID() zero = %v, want ErrNotFound", err)
	}

	if err := store.Remove("gate"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, ok := store.FindByID(front.ID); ok {
		t.Fatalf("removed camera still found")
	}
	if err := store.Remove("gate"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Remove() missing = %v, want ErrNotFound", err)
	}
	if err := store.RemoveByID(back.ID); err != nil {
		t.Fatalf("RemoveByID() error = %v", err)
	}
	if err := store.RemoveByID(back.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RemoveByID() missing = %v, want ErrNotFound", err)
	}
}
