package miner

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jellybelly420/pearl-netbar-miner/internal/config"
)

func Ensure(ctx context.Context, spec config.Miner) (string, error) {
	target := filepath.Join(spec.InstallDir, spec.Executable)
	if info, err := os.Stat(target); err == nil && !info.IsDir() {
		actual, hashErr := fileSHA256(target)
		if hashErr != nil {
			return "", hashErr
		}
		if actual == strings.ToLower(spec.ExecutableSHA256) {
			return target, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(spec.InstallDir), 0o755); err != nil {
		return "", err
	}
	temporary, err := os.CreateTemp(filepath.Dir(spec.InstallDir), "wildrig-*.zip")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	temporary.Close()
	defer os.Remove(temporaryPath)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.URL, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("miner download returned %s", response.Status)
	}
	file, err := os.OpenFile(temporaryPath, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, 512<<20))
	closeErr := file.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != strings.ToLower(spec.SHA256) {
		return "", fmt.Errorf("miner SHA-256 mismatch: got %s", actual)
	}

	staging := spec.InstallDir + ".staging"
	_ = os.RemoveAll(staging)
	if err := extractExecutable(temporaryPath, staging, spec.Executable); err != nil {
		return "", err
	}
	_ = os.RemoveAll(spec.InstallDir)
	if err := os.Rename(staging, spec.InstallDir); err != nil {
		return "", err
	}
	actualExecutable, err := fileSHA256(target)
	if err != nil {
		return "", err
	}
	if actualExecutable != strings.ToLower(spec.ExecutableSHA256) {
		_ = os.RemoveAll(spec.InstallDir)
		return "", fmt.Errorf("miner executable SHA-256 mismatch: got %s", actualExecutable)
	}
	return target, nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(file, 256<<20)); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func extractExecutable(archive, destination, executable string) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer reader.Close()
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	found := false
	for _, entry := range reader.File {
		clean := filepath.Clean(entry.Name)
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return errors.New("miner ZIP contains an unsafe path")
		}
		if !strings.EqualFold(filepath.Base(clean), executable) {
			continue
		}
		input, err := entry.Open()
		if err != nil {
			return err
		}
		outputPath := filepath.Join(destination, executable)
		output, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			input.Close()
			return err
		}
		_, copyErr := io.Copy(output, io.LimitReader(input, 256<<20))
		closeOutErr := output.Close()
		closeInErr := input.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeOutErr != nil {
			return closeOutErr
		}
		if closeInErr != nil {
			return closeInErr
		}
		found = true
		break
	}
	if !found {
		return fmt.Errorf("%s not found in miner ZIP", executable)
	}
	return nil
}
