package miner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jellybelly420/pearl-netbar-miner/internal/config"
)

func TestPinnedWildRigArtifact(t *testing.T) {
	archive := os.Getenv("PEARL_WILDRIG_ARCHIVE")
	if archive == "" {
		t.Skip("set PEARL_WILDRIG_ARCHIVE to run the official artifact integration test")
	}
	archive, err := filepath.Abs(archive)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.ServeFile(writer, request, archive)
	}))
	defer server.Close()

	installDir := filepath.Join(t.TempDir(), "wildrig-0.51.4")
	executable, err := Ensure(context.Background(), config.Miner{
		Version: "0.51.4", URL: server.URL + "/wildrig.zip",
		SHA256:     "25b8cf133ee513a9b82d87bab7e277aff32d4472aac79c4ab3053d7653867c0d",
		Executable: "wildrig.exe", ExecutableSHA256: "c656c5c29c98b2d9167f73d9f9237823f790f5aadd3848322c5997a257798788",
		InstallDir: installDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if executable != filepath.Join(installDir, "wildrig.exe") {
		t.Fatalf("unexpected executable path %q", executable)
	}
	if actual, err := fileSHA256(executable); err != nil || actual != "c656c5c29c98b2d9167f73d9f9237823f790f5aadd3848322c5997a257798788" {
		t.Fatalf("unexpected executable digest %q, %v", actual, err)
	}
}
