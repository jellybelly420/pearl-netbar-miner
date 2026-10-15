package protocol

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	HeaderAgent     = "X-Pearl-Agent"
	HeaderTimestamp = "X-Pearl-Timestamp"
	HeaderNonce     = "X-Pearl-Nonce"
	HeaderSignature = "X-Pearl-Signature"
)

func SignRequest(request *http.Request, secret string, now time.Time) error {
	body, err := bodyBytes(request)
	if err != nil {
		return err
	}
	nonceBytes := make([]byte, 18)
	if _, err := rand.Read(nonceBytes); err != nil {
		return err
	}
	timestamp := strconv.FormatInt(now.Unix(), 10)
	nonce := hex.EncodeToString(nonceBytes)
	request.Header.Set(HeaderTimestamp, timestamp)
	request.Header.Set(HeaderNonce, nonce)
	request.Header.Set(HeaderSignature, signature(request.Method, request.URL.RequestURI(), timestamp, nonce, body, secret))
	return nil
}

func VerifyRequest(request *http.Request, secret string, now time.Time, maxSkew time.Duration) ([]byte, error) {
	timestamp := request.Header.Get(HeaderTimestamp)
	nonce := request.Header.Get(HeaderNonce)
	provided := request.Header.Get(HeaderSignature)
	if timestamp == "" || nonce == "" || provided == "" {
		return nil, errors.New("missing request signature headers")
	}
	unix, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return nil, errors.New("invalid signature timestamp")
	}
	delta := now.Sub(time.Unix(unix, 0))
	if delta < -maxSkew || delta > maxSkew {
		return nil, errors.New("signature timestamp outside allowed window")
	}
	body, err := bodyBytes(request)
	if err != nil {
		return nil, err
	}
	expected := signature(request.Method, request.URL.RequestURI(), timestamp, nonce, body, secret)
	if !hmac.Equal([]byte(strings.ToLower(provided)), []byte(expected)) {
		return nil, errors.New("invalid request signature")
	}
	return body, nil
}

func signature(method, uri, timestamp, nonce string, body []byte, secret string) string {
	bodyHash := sha256.Sum256(body)
	canonical := fmt.Sprintf("%s\n%s\n%s\n%s\n%s", method, uri, timestamp, nonce, hex.EncodeToString(bodyHash[:]))
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

func bodyBytes(request *http.Request) ([]byte, error) {
	if request.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	request.Body.Close()
	request.Body = io.NopCloser(strings.NewReader(string(body)))
	return body, nil
}
