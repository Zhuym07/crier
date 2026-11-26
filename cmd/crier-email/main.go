// Command crier-email is the email source plugin.
//
// crier starts it once per call and hands it one JSON-RPC 2.0 request on
// stdin, one per line. The plugin writes one response line on stdout and
// exits, so a hung mailbox can never outlive its call.
//
// Methods:
//
//	test  log in and select the mailbox
//	poll  hand over the mail that arrived since the cursor
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/Zhuym07/crier/internal/mail"
)

// JSON-RPC 2.0 error codes.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeUpstream       = -32000
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

// params are the request fields; each method uses the ones it needs.
type params struct {
	Address  string `json:"address"`
	Username string `json:"username"`
	Secret   string `json:"secret"`
	Mailbox  string `json:"mailbox"`
	Cursor   string `json:"cursor"`
}

type testResult struct {
	Mailbox string `json:"mailbox"`
}

func main() {
	if err := run(os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "crier-email:", err)
		os.Exit(1)
	}
}

// run reads the one request line and writes the one response line.
func run(in io.Reader, out io.Writer, errOut io.Writer) error {
	line, err := bufio.NewReader(in).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return fmt.Errorf("read request: %w", err)
	}

	encoder := json.NewEncoder(out)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(handle(line, errOut))
}

func handle(line []byte, errOut io.Writer) response {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return failure(nil, codeParseError, "invalid JSON: "+err.Error())
	}
	if req.JSONRPC != "2.0" || len(req.ID) == 0 {
		return failure(req.ID, codeInvalidRequest, "not a JSON-RPC 2.0 request with an id")
	}

	var p params
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return failure(req.ID, codeInvalidParams, err.Error())
		}
	}

	switch req.Method {
	case "test":
		if err := p.check(); err != nil {
			return failure(req.ID, codeInvalidParams, err.Error())
		}
		mailbox, err := mail.Test(p.config(errOut))
		if err != nil {
			fmt.Fprintln(errOut, "crier-email: test:", err)
			return failure(req.ID, codeUpstream, err.Error())
		}
		return success(req.ID, testResult{Mailbox: mailbox})
	case "poll":
		if err := p.check(); err != nil {
			return failure(req.ID, codeInvalidParams, err.Error())
		}
		result, err := mail.Poll(p.config(errOut), p.Cursor)
		if errors.Is(err, mail.ErrCursor) {
			return failure(req.ID, codeInvalidParams, err.Error())
		}
		if err != nil {
			fmt.Fprintln(errOut, "crier-email: poll:", err)
			return failure(req.ID, codeUpstream, err.Error())
		}
		return success(req.ID, result)
	default:
		return failure(req.ID, codeMethodNotFound, "unknown method "+req.Method)
	}
}

// check reports the account fields every method needs. The secret is never
// echoed, only its absence.
func (p params) check() error {
	switch {
	case p.Address == "":
		return errors.New("address is required")
	case p.Username == "":
		return errors.New("username is required")
	case p.Secret == "":
		return errors.New("secret is required")
	}
	return nil
}

func (p params) config(diagnostics io.Writer) mail.Config {
	return mail.Config{
		Address:  p.Address,
		Username: p.Username,
		Secret:   p.Secret,
		Mailbox:  p.Mailbox,
		Log:      diagnostics,
	}
}

func success(id json.RawMessage, result any) response {
	return response{JSONRPC: "2.0", ID: id, Result: result}
}

func failure(id json.RawMessage, code int, message string) response {
	return response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}}
}
