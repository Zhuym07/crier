// Command crier-telegram is an optional JSON-RPC source plugin for Telegram.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/Zhuym07/crier/internal/telegram"
	"io"
	"os"
	"time"
)

const (
	parseError     = -32700
	invalidRequest = -32600
	methodNotFound = -32601
	invalidParams  = -32602
	upstream       = -32000
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type params struct {
	Token               string  `json:"token"`
	ProxyURL            string  `json:"proxy_url"`
	BatchTimeoutSeconds int     `json:"batch_timeout_seconds"`
	MaxPoints           float64 `json:"max_points"`
	RechargePerSec      float64 `json:"recharge_per_sec"`
	WhitelistUserIDs    []int64 `json:"whitelist_user_ids"`
	LogChannelID        int64   `json:"log_channel_id"`
	TodoChannelID       int64   `json:"todo_channel_id"`
	ImageCharsPerLine   int     `json:"chars_per_line"`
	LineHeightPx        int     `json:"line_height_px"`
	Cursor              string  `json:"cursor"`
	ChatID              int64   `json:"chat_id"`
	Text                string  `json:"text"`
}

func main() {
	if e := run(os.Stdin, os.Stdout, os.Stderr); e != nil {
		fmt.Fprintln(os.Stderr, "crier-telegram:", e)
		os.Exit(1)
	}
}
func run(in io.Reader, out, diag io.Writer) error {
	line, e := bufio.NewReader(in).ReadBytes('\n')
	if e != nil && len(line) == 0 {
		return e
	}
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	return enc.Encode(handle(line, diag))
}
func handle(line []byte, diag io.Writer) response {
	var q request
	if e := json.Unmarshal(line, &q); e != nil {
		return fail(nil, parseError, "invalid JSON")
	}
	if q.JSONRPC != "2.0" || len(q.ID) == 0 {
		return fail(q.ID, invalidRequest, "not a JSON-RPC 2.0 request with an id")
	}
	if q.Method != "poll" && q.Method != "notify" {
		return fail(q.ID, methodNotFound, "unknown method "+q.Method)
	}
	var p params
	if len(q.Params) > 0 {
		if e := json.Unmarshal(q.Params, &p); e != nil {
			return fail(q.ID, invalidParams, e.Error())
		}
	}
	if p.Token == "" {
		return fail(q.ID, invalidParams, "token is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	cfg := telegram.Config{Token: p.Token, ProxyURL: p.ProxyURL, BatchTimeoutSeconds: p.BatchTimeoutSeconds, MaxPoints: p.MaxPoints, RechargePerSec: p.RechargePerSec, WhitelistUserIDs: p.WhitelistUserIDs, LogChannelID: p.LogChannelID, TodoChannelID: p.TodoChannelID, ImageCharsPerLine: p.ImageCharsPerLine, LineHeightPx: p.LineHeightPx, Log: diag}
	if q.Method == "notify" {
		if err := telegram.Notify(ctx, cfg, p.ChatID, p.Text); err != nil {
			return fail(q.ID, upstream, err.Error())
		}
		return response{JSONRPC: "2.0", ID: q.ID, Result: true}
	}
	r, e := telegram.Poll(ctx, cfg, p.Cursor)
	if e != nil {
		fmt.Fprintln(diag, "crier-telegram: poll:", e)
		return fail(q.ID, upstream, e.Error())
	}
	return response{JSONRPC: "2.0", ID: q.ID, Result: r}
}
func fail(id json.RawMessage, code int, msg string) response {
	return response{JSONRPC: "2.0", ID: id, Error: &rpcError{code, msg}}
}
