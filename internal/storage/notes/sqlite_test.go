package notes

import (
	"context"
	"errors"
	ledgerstore "nyan-go/internal/storage/sqlite"
	"path/filepath"
	"strings"
	"testing"
)

func TestNotesPersistAndSupportCRUD(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.sqlite")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Create(context.Background(), "Xin chào 🐈")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Create(context.Background(), "Ghi chú mới")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	listed, err := store.List(context.Background(), 0, MaxPageSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 || listed[0].ID != second.ID || listed[1].ID != first.ID || listed[1].Content != "Xin chào 🐈" {
		t.Fatalf("notes after reopening = %#v", listed)
	}
	updated, err := store.Update(context.Background(), first.ID, "Đã sửa ✨")
	if err != nil || updated.Content != "Đã sửa ✨" || updated.UpdatedAt.Before(updated.CreatedAt) {
		t.Fatalf("updated=%#v err=%v", updated, err)
	}
	deleted, err := store.Delete(context.Background(), second.ID)
	if err != nil || deleted.Content != "Ghi chú mới" || deleted.ID != second.ID {
		t.Fatal(err)
	}
	if _, err := store.Update(context.Background(), second.ID, "không còn"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("updating deleted note error = %v, want ErrNotFound", err)
	}
}

func TestNotesDatabaseIsSeparateFromLedger(t *testing.T) {
	dir := t.TempDir()
	ledger, err := ledgerstore.Open(filepath.Join(dir, "ledger.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	notes, err := Open(filepath.Join(dir, "notes.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer notes.Close()
	var ledgerNotes int
	if err := ledger.DB().QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='notes'`).Scan(&ledgerNotes); err != nil {
		t.Fatal(err)
	}
	if ledgerNotes != 0 {
		t.Fatal("ledger database unexpectedly contains notes table")
	}
	var notesTransactions int
	if err := notes.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='transactions'`).Scan(&notesTransactions); err != nil {
		t.Fatal(err)
	}
	if notesTransactions != 0 {
		t.Fatal("notes database unexpectedly contains ledger table")
	}
}

func TestNotesRejectInvalidContentAndBoundList(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "notes.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, content := range []string{"", " \n\t", strings.Repeat("a", MaxRunes+1)} {
		if _, err := store.Create(context.Background(), content); !errors.Is(err, ErrInvalid) {
			t.Fatalf("Create(%d runes) error=%v, want ErrInvalid", len([]rune(content)), err)
		}
	}
	for _, tc := range []struct{ offset, limit int }{{-1, 10}, {0, 0}, {0, MaxPageSize + 2}} {
		if _, err := store.List(context.Background(), tc.offset, tc.limit); !errors.Is(err, ErrInvalid) {
			t.Fatalf("List(%d,%d) error=%v, want ErrInvalid", tc.offset, tc.limit, err)
		}
	}
}
