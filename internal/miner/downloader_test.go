package miner

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractExecutableRejectsTraversal(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "bad.zip")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("../wildrig.exe")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte("bad"))
	_ = writer.Close()
	_ = file.Close()
	if err := extractExecutable(archive, filepath.Join(t.TempDir(), "out"), "wildrig.exe"); err == nil {
		t.Fatal("expected ZIP traversal to be rejected")
	}
}

func TestExtractExecutableFindsNestedBinary(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "ok.zip")
	file, _ := os.Create(archive)
	writer := zip.NewWriter(file)
	entry, _ := writer.Create("wildrig-0.51.4/wildrig.exe")
	_, _ = entry.Write([]byte("binary"))
	_ = writer.Close()
	_ = file.Close()
	out := filepath.Join(t.TempDir(), "out")
	if err := extractExecutable(archive, out, "wildrig.exe"); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(out, "wildrig.exe"))
	if err != nil || string(contents) != "binary" {
		t.Fatalf("unexpected extraction: %q, %v", contents, err)
	}
}
