package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// backupFile copies src into dir preserving the file name with a timestamp.
// Creates dir if needed. Returns error if copy fails.
func backupFile(src string, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := filepath.Base(src)
	ext := filepath.Ext(name)
	base := name[:len(name)-len(ext)]
	ts := time.Now().Format("20060102_150405")
	dst := filepath.Join(dir, fmt.Sprintf("%s_%s%s", base, ts, ext))

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
