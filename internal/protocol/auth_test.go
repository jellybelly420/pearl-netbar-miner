package protocol

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSignAndVerify(t *testing.T) {
	now := time.Unix(1700000000, 0)
	request, _ := http.NewRequest(http.MethodPost, "https://example.com/api?after=1", bytes.NewBufferString(`{"ok":true}`))
	if err := SignRequest(request, "01234567890123456789012345678901", now); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyRequest(request, "01234567890123456789012345678901", now.Add(time.Minute), 5*time.Minute); err != nil {
		t.Fatal(err)
	}
}

func TestRejectsOversizedBody(t *testing.T) {
	request, _ := http.NewRequest(http.MethodPost, "https://example.com/api", strings.NewReader(strings.Repeat("a", maxBodyBytes+1)))
	if err := SignRequest(request, "01234567890123456789012345678901", time.Now()); err == nil {
		t.Fatal("expected oversized request body to fail")
	}
}

func TestVerifyRejectsTampering(t *testing.T) {
	now := time.Unix(1700000000, 0)
	request, _ := http.NewRequest(http.MethodPost, "https://example.com/api", bytes.NewBufferString("original"))
	_ = SignRequest(request, "01234567890123456789012345678901", now)
	request.Body = ioBody("changed")
	if _, err := VerifyRequest(request, "01234567890123456789012345678901", now, 5*time.Minute); err == nil {
		t.Fatal("expected tampered body to fail")
	}
}

type stringBody struct{ *bytes.Reader }

func (stringBody) Close() error { return nil }

func ioBody(value string) stringBody { return stringBody{bytes.NewReader([]byte(value))} }
