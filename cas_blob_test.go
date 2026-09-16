package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestBlobSurvivesDiskLoss(t *testing.T) {
	dir := t.TempDir()
	oldMedia, oldPrefix := mediaDir, publicPrefix
	mediaDir, publicPrefix = dir, "/media"
	defer func() { mediaDir, publicPrefix = oldMedia, oldPrefix }()

	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := setupMediaTable(db); err != nil {
		t.Fatal(err)
	}
	b := &bridge{logs: &logStore{}, stateDB: db, stateWriteCh: make(chan stateWriteJob, 16)}
	go func() {
		for range b.stateWriteCh {
		}
	}()

	pub, err := b.saveBytesToCAS([]byte("<svg>hi</svg>"), ".svg", "c", "m")
	if err != nil {
		t.Fatal(err)
	}
	fp := filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(pub, "/media/")))
	if err := os.Remove(fp); err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(fp)
	hash := strings.TrimSuffix(base, filepath.Ext(base))
	data, ext := b.loadBlobFromDB(hash)
	if string(data) != "<svg>hi</svg>" || ext != ".svg" {
		t.Fatalf("db roundtrip = %q %q", data, ext)
	}
	listed := b.listBlobHashes(10)
	if len(listed) != 1 || listed[0][0] != hash {
		t.Fatalf("list = %v want [%s]", listed, hash)
	}
}
