package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jellybelly420/pearl-netbar-miner/internal/config"
)

type Manifest struct {
	Schema      int          `json:"schema"`
	Channel     string       `json:"channel"`
	GeneratedAt time.Time    `json:"generated_at"`
	Miner       config.Miner `json:"miner"`
	Signature   string       `json:"signature,omitempty"`
}

type signedPayload struct {
	Schema      int          `json:"schema"`
	Channel     string       `json:"channel"`
	GeneratedAt time.Time    `json:"generated_at"`
	Miner       config.Miner `json:"miner"`
}

func Sign(manifest Manifest, privateKey ed25519.PrivateKey) (Manifest, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return Manifest{}, errors.New("invalid Ed25519 private key")
	}
	canonical, err := canonicalBytes(manifest)
	if err != nil {
		return Manifest{}, err
	}
	manifest.Signature = base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, canonical))
	return manifest, nil
}

func Verify(contents []byte, publicKeyText string) (Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(contents, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode update manifest: %w", err)
	}
	publicKey, err := decodeKey(publicKeyText)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return Manifest{}, errors.New("invalid Ed25519 public key")
	}
	signature, err := decodeKey(manifest.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return Manifest{}, errors.New("invalid update manifest signature encoding")
	}
	canonical, err := canonicalBytes(manifest)
	if err != nil {
		return Manifest{}, err
	}
	if !ed25519.Verify(ed25519.PublicKey(publicKey), canonical, signature) {
		return Manifest{}, errors.New("update manifest signature verification failed")
	}
	if manifest.Schema != 1 {
		return Manifest{}, fmt.Errorf("unsupported update manifest schema %d", manifest.Schema)
	}
	if manifest.GeneratedAt.IsZero() {
		return Manifest{}, errors.New("update manifest generated_at is required")
	}
	if err := config.ValidateMiner(manifest.Miner); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func Fetch(ctx context.Context, manifestURL, publicKey string) (Manifest, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return Manifest{}, err
	}
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		return Manifest{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Manifest{}, fmt.Errorf("update manifest returned %s", response.Status)
	}
	contents, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return Manifest{}, err
	}
	return Verify(contents, publicKey)
}

func Eligible(agentID string, canaryPercent int) bool {
	if canaryPercent >= 100 {
		return true
	}
	if canaryPercent <= 0 {
		return false
	}
	digest := sha256.Sum256([]byte(agentID))
	bucket := int(binary.BigEndian.Uint16(digest[:2]) % 100)
	return bucket < canaryPercent
}

func canonicalBytes(manifest Manifest) ([]byte, error) {
	return json.Marshal(signedPayload{
		Schema: manifest.Schema, Channel: manifest.Channel,
		GeneratedAt: manifest.GeneratedAt.UTC(), Miner: manifest.Miner,
	})
}

func decodeKey(value string) ([]byte, error) {
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		decoded, err := encoding.DecodeString(value)
		if err == nil {
			return decoded, nil
		}
	}
	return nil, errors.New("invalid base64")
}
