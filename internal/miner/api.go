package miner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

type Statistics struct {
	Hashrate10s float64 `json:"hashrate_10s"`
	Hashrate60s float64 `json:"hashrate_60s"`
	Hashrate15m float64 `json:"hashrate_15m"`
	Highest     float64 `json:"hashrate_highest"`
	Accepted    int64   `json:"accepted_shares"`
	Rejected    int64   `json:"rejected_shares"`
	Pool        string  `json:"pool"`
	Uptime      int64   `json:"uptime_seconds"`
}

type apiResponse struct {
	Uptime   int64 `json:"uptime"`
	Hashrate struct {
		Total   []*float64 `json:"total"`
		Highest float64    `json:"highest"`
	} `json:"hashrate"`
	Results struct {
		SharesGood  int64 `json:"shares_good"`
		SharesTotal int64 `json:"shares_total"`
	} `json:"results"`
	Connection struct {
		Pool string `json:"pool"`
	} `json:"connection"`
}

func FetchStatistics(ctx context.Context, port int) (Statistics, error) {
	if port < 1024 || port > 65535 {
		return Statistics{}, fmt.Errorf("invalid local miner API port %d", port)
	}
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+"/", nil)
	if err != nil {
		return Statistics{}, err
	}
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return Statistics{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Statistics{}, fmt.Errorf("miner API returned %s", response.Status)
	}
	var payload apiResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err := decoder.Decode(&payload); err != nil {
		return Statistics{}, fmt.Errorf("decode miner API: %w", err)
	}
	statistics := Statistics{
		Highest: payload.Hashrate.Highest, Accepted: payload.Results.SharesGood,
		Pool: payload.Connection.Pool, Uptime: payload.Uptime,
	}
	if payload.Results.SharesTotal >= payload.Results.SharesGood {
		statistics.Rejected = payload.Results.SharesTotal - payload.Results.SharesGood
	}
	statistics.Hashrate10s = rateAt(payload.Hashrate.Total, 0)
	statistics.Hashrate60s = rateAt(payload.Hashrate.Total, 1)
	statistics.Hashrate15m = rateAt(payload.Hashrate.Total, 2)
	return statistics, nil
}

func rateAt(values []*float64, index int) float64 {
	if index >= len(values) || values[index] == nil {
		return 0
	}
	return *values[index]
}
