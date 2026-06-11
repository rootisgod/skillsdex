package skillsdex

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func extractZipToDir(zr *zip.Reader, dir string, confirm confirmOptions) (int, error) {
	rootAbs, err := filepath.Abs(dir)
	if err != nil {
		return 0, err
	}
	written := 0
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		dst, err := safeZipDestination(rootAbs, f.Name)
		if err != nil {
			return written, err
		}
		content, err := readZipEntryLimited(f, maxImportedSkillBytes)
		if err != nil {
			return written, err
		}
		if err := confirmFileOverwrite(dst, content, confirm); err != nil {
			return written, err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
			return written, err
		}
		if err := os.WriteFile(dst, content, 0600); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}

func safeZipDestination(rootAbs, name string) (string, error) {
	if strings.TrimSpace(name) == "" || strings.Contains(name, "\\") || path.IsAbs(name) {
		return "", fmt.Errorf("unsafe export path %q", name)
	}
	clean := path.Clean(name)
	if clean == "." || clean != name || strings.HasPrefix(clean, "../") || clean == ".." || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("unsafe export path %q", name)
	}
	dst := filepath.Join(rootAbs, filepath.FromSlash(clean))
	dstAbs, err := filepath.Abs(dst)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, dstAbs)
	if err != nil {
		return "", err
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", fmt.Errorf("unsafe export path %q", name)
	}
	return dstAbs, nil
}

func readZipEntryLimited(f *zip.File, limit int64) ([]byte, error) {
	if f.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("%s is too large", f.Name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	content, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > limit {
		return nil, fmt.Errorf("%s is too large", f.Name)
	}
	return content, nil
}
