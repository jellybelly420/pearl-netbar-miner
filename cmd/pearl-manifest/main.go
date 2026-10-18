package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	updatepkg "github.com/jellybelly420/pearl-netbar-miner/internal/update"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: pearl-manifest <keygen|sign|verify>")
	}
	switch os.Args[1] {
	case "keygen":
		keygen(os.Args[2:])
	case "sign":
		sign(os.Args[2:])
	case "verify":
		verify(os.Args[2:])
	default:
		log.Fatalf("unknown command %q", os.Args[1])
	}
}

func verify(arguments []string) {
	flags := flag.NewFlagSet("verify", flag.ExitOnError)
	publicPath := flags.String("public", "", "public key file")
	inputPath := flags.String("input", "", "signed manifest JSON")
	_ = flags.Parse(arguments)
	if *publicPath == "" || *inputPath == "" {
		log.Fatal("public and input are required")
	}
	publicKey, err := os.ReadFile(*publicPath)
	if err != nil {
		log.Fatal(err)
	}
	contents, err := os.ReadFile(*inputPath)
	if err != nil {
		log.Fatal(err)
	}
	manifest, err := updatepkg.Verify(contents, strings.TrimSpace(string(publicKey)))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("verified channel=%s miner=%s generated_at=%s\n", manifest.Channel, manifest.Miner.Version, manifest.GeneratedAt.UTC().Format("2006-01-02T15:04:05Z"))
}

func keygen(arguments []string) {
	flags := flag.NewFlagSet("keygen", flag.ExitOnError)
	privatePath := flags.String("private", "manifest-private.key", "new private-key output file")
	publicPath := flags.String("public", "manifest-public.key", "new public-key output file")
	_ = flags.Parse(arguments)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatal(err)
	}
	if err := writeExclusive(*privatePath, []byte(base64.RawStdEncoding.EncodeToString(privateKey)+"\n"), 0o600); err != nil {
		log.Fatal(err)
	}
	if err := writeExclusive(*publicPath, []byte(base64.RawStdEncoding.EncodeToString(publicKey)+"\n"), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("created private key %s and public key %s\n", *privatePath, *publicPath)
}

func sign(arguments []string) {
	flags := flag.NewFlagSet("sign", flag.ExitOnError)
	privatePath := flags.String("private", "", "private key file")
	inputPath := flags.String("input", "", "unsigned manifest JSON")
	outputPath := flags.String("output", "", "new signed manifest JSON")
	_ = flags.Parse(arguments)
	if *privatePath == "" || *inputPath == "" || *outputPath == "" {
		log.Fatal("private, input, and output are required")
	}
	privateText, err := os.ReadFile(*privatePath)
	if err != nil {
		log.Fatal(err)
	}
	privateKey, err := base64.RawStdEncoding.DecodeString(strings.TrimSpace(string(privateText)))
	if err != nil || len(privateKey) != ed25519.PrivateKeySize {
		log.Fatal("private key file is not a valid Ed25519 key")
	}
	contents, err := os.ReadFile(*inputPath)
	if err != nil {
		log.Fatal(err)
	}
	var manifest updatepkg.Manifest
	if err := json.Unmarshal(contents, &manifest); err != nil {
		log.Fatal(err)
	}
	signed, err := updatepkg.Sign(manifest, ed25519.PrivateKey(privateKey))
	if err != nil {
		log.Fatal(err)
	}
	encoded, err := json.MarshalIndent(signed, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	encoded = append(encoded, '\n')
	if err := writeExclusive(*outputPath, encoded, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("created signed manifest %s\n", *outputPath)
}

func writeExclusive(path string, contents []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
