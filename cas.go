package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func setupMediaTable(db *sql.DB) error {
	if db == nil {
		return nil
	}
	if isPostgres() {
		_, err := db.Exec(`CREATE TABLE IF NOT EXISTS media_blob(hash TEXT PRIMARY KEY, ext TEXT NOT NULL, bytes BYTEA NOT NULL, created BIGINT NOT NULL)`)
		if err != nil {
			return err
		}
		_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_media_blob_created ON media_blob(created DESC)`)
		return err
	}
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS media_blob(hash TEXT PRIMARY KEY, ext TEXT NOT NULL, bytes BLOB NOT NULL, created INTEGER NOT NULL);`)
	if err != nil {
		return err
	}
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_media_blob_created ON media_blob(created DESC);`)
	return nil
}

func (b *bridge) saveBlobToDB(hash, ext string, data []byte) {
	if b == nil || b.stateDB == nil || len(data) == 0 || len(data) > 20<<20 {
		return
	}
	now := time.Now().Unix()
	b.dbMu.Lock()
	defer b.dbMu.Unlock()
	if isPostgres() {
		_, _ = b.stateDB.Exec(`INSERT INTO media_blob(hash, ext, bytes, created) VALUES($1,$2,$3,$4) ON CONFLICT(hash) DO NOTHING`, hash, ext, data, now)
	} else {
		_, _ = b.stateDB.Exec(`INSERT OR IGNORE INTO media_blob(hash, ext, bytes, created) VALUES(?,?,?,?)`, hash, ext, data, now)
	}
}

func (b *bridge) loadBlobFromDB(hash string) ([]byte, string) {
	if b == nil || b.stateDB == nil || hash == "" {
		return nil, ""
	}
	b.dbMu.Lock()
	defer b.dbMu.Unlock()
	var ext string
	var data []byte
	var err error
	if isPostgres() {
		err = b.stateDB.QueryRow(`SELECT ext, bytes FROM media_blob WHERE hash=$1`, hash).Scan(&ext, &data)
	} else {
		err = b.stateDB.QueryRow(`SELECT ext, bytes FROM media_blob WHERE hash=?`, hash).Scan(&ext, &data)
	}
	if err != nil || len(data) == 0 {
		return nil, ""
	}
	return data, ext
}

func (b *bridge) listBlobHashes(limit int) [][2]string {
	if b == nil || b.stateDB == nil {
		return nil
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	b.dbMu.Lock()
	defer b.dbMu.Unlock()
	var rows *sql.Rows
	var err error
	if isPostgres() {
		rows, err = b.stateDB.Query(`SELECT hash, ext FROM media_blob ORDER BY created DESC LIMIT $1`, limit)
	} else {
		rows, err = b.stateDB.Query(`SELECT hash, ext FROM media_blob ORDER BY created DESC LIMIT ?`, limit)
	}
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out [][2]string
	for rows.Next() {
		var h, e string
		if err := rows.Scan(&h, &e); err != nil {
			continue
		}
		out = append(out, [2]string{h, e})
	}
	return out
}

func (b *bridge) saveBytesToCAS(data []byte, ext string, chatID string, msgID string) (string, error) {
	if ext == "" {
		ext = ".bin"
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	ext = strings.ToLower(ext)
	cleanExt := "."
	for _, r := range strings.TrimPrefix(ext, ".") {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			cleanExt += string(r)
		}
	}
	ext = cleanExt
	if ext == "." {
		ext = ".bin"
	}
	h := sha256.Sum256(data)
	hexHash := hex.EncodeToString(h[:])
	shardDir := filepath.Join(mediaDir, hexHash[0:2], hexHash[2:4])
	fname := hexHash + ext
	fname = filepath.Base(fname)
	diskPath := filepath.Join(shardDir, fname)
	absMedia, _ := filepath.Abs(mediaDir)
	absPath, _ := filepath.Abs(diskPath)
	if absPath != absMedia && !strings.HasPrefix(absPath, absMedia+string(os.PathSeparator)) {
		return "", fmt.Errorf("path traversal blocked: %s", diskPath)
	}
	if err := os.MkdirAll(shardDir, 0755); err != nil {
		return "", err
	}
	if _, err := os.Stat(diskPath); err == nil {
		b.saveBlobToDB(hexHash, ext, data)
		pub := publicPrefix + "/" + hexHash[0:2] + "/" + hexHash[2:4] + "/" + fname
		return pub, nil
	}
	if err := os.WriteFile(diskPath, data, 0644); err != nil {
		return "", err
	}
	b.saveBlobToDB(hexHash, ext, data)
	pub := publicPrefix + "/" + hexHash[0:2] + "/" + hexHash[2:4] + "/" + fname
	return pub, nil
}
