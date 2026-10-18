package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jellybelly420/pearl-netbar-miner/internal/control"
	"github.com/jellybelly420/pearl-netbar-miner/internal/protocol"
)

type Client struct {
	baseURL string
	id      string
	secret  string
	http    *http.Client
}

func NewClient(baseURL, id, secret string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), id: id, secret: secret, http: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) Heartbeat(heartbeat control.Heartbeat) error {
	return c.doJSON(http.MethodPost, "/api/v1/agent/heartbeat", heartbeat, nil)
}

func (c *Client) Commands(after uint64) ([]control.Command, error) {
	var commands []control.Command
	path := "/api/v1/agent/commands?after=" + url.QueryEscape(strconv.FormatUint(after, 10))
	err := c.doJSON(http.MethodGet, path, nil, &commands)
	return commands, err
}

func (c *Client) Ack(sequence uint64, acknowledgement control.CommandAck) error {
	return c.doJSON(http.MethodPost, fmt.Sprintf("/api/v1/agent/commands/%d/ack", sequence), acknowledgement, nil)
}

func (c *Client) doJSON(method, path string, input, output any) error {
	var body []byte
	var err error
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			return err
		}
	}
	request, err := http.NewRequest(method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(protocol.HeaderAgent, c.id)
	if err := protocol.SignRequest(request, c.secret, time.Now()); err != nil {
		return err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("control service returned %s", response.Status)
	}
	if output != nil {
		return json.NewDecoder(response.Body).Decode(output)
	}
	return nil
}
