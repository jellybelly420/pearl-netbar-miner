package update

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/jellybelly420/pearl-netbar-miner/internal/config"
)

func TestSignAndVerify(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := Sign(Manifest{
		Schema: 1, Channel: "stable", GeneratedAt: time.Unix(1000, 0),
		Miner: config.Miner{
			Version: "1", URL: "https://example.com/miner.zip",
			SHA256:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Executable: "wildrig.exe", ExecutableSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			InstallDir: "miners/wildrig-1",
		},
	}, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	contents, _ := json.Marshal(manifest)
	verified, err := Verify(contents, base64.RawStdEncoding.EncodeToString(publicKey))
	if err != nil || verified.Miner.Version != "1" {
		t.Fatalf("verify failed: %+v %v", verified, err)
	}
	manifest.Miner.Version = "tampered"
	contents, _ = json.Marshal(manifest)
	if _, err := Verify(contents, base64.RawStdEncoding.EncodeToString(publicKey)); err == nil {
		t.Fatal("expected tampered manifest to fail")
	}
}

func TestEligibleIsDeterministic(t *testing.T) {
	first := Eligible("seat-001", 10)
	for index := 0; index < 100; index++ {
		if Eligible("seat-001", 10) != first {
			t.Fatal("canary assignment changed")
		}
	}
	if !Eligible("seat-001", 100) || Eligible("seat-001", 0) {
		t.Fatal("canary boundaries are incorrect")
	}
}
