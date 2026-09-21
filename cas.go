package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func setupMediaTable(db *sql.DB) error {
	// disabled: media bytes not stored in DB to save free-tier quota
	// filesystem-only (ephemeral on Koyeb) - 404 after restart is expected
	return nil
}

func (b *bridge) saveBlobToDB(hash, ext string, data []byte) {
	// disabled to save free-tier DB quota - filesystem only
}

func (b *bridge) loadBlobFromDB(hash string) ([]byte, string) {
	// disabled to save free-tier DB quota
	return nil, ""
}

func (b *bridge) listBlobHashes(limit int) [][2]string {
	// disabled to save free-tier DB quota
	return nil
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
		pub := publicPrefix + "/" + hexHash[0:2] + "/" + hexHash[2:4] + "/" + fname
		return pub, nil
	}
	if err := os.WriteFile(diskPath, data, 0644); err != nil {
		return "", err
	}
	pub := publicPrefix + "/" + hexHash[0:2] + "/" + hexHash[2:4] + "/" + fname
	return pub, nil
}
