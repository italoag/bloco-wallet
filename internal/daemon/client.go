package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// Client is a private daemon RPC client.
type Client struct {
	conn    net.Conn
	reader  *bufio.Reader
	token   string
	timeout time.Duration
}

// Dial connects to the daemon address and authenticates with the token.
func Dial(ctx context.Context, address, token string) (*Client, error) {
	if address == "" || token == "" {
		return nil, fmt.Errorf("daemon: address and token are required")
	}
	network := "unix"
	if strings.HasPrefix(address, "127.0.0.1:") || strings.HasPrefix(address, "[::1]:") || strings.HasPrefix(address, "localhost:") {
		network = "tcp"
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, network, address)
	if err != nil {
		return nil, fmt.Errorf("daemon: dial: %w", err)
	}
	return &Client{conn: conn, reader: bufio.NewReader(conn), token: token, timeout: 30 * time.Second}, nil
}

// Close terminates the connection.
func (client *Client) Close() error {
	if client == nil || client.conn == nil {
		return nil
	}
	return client.conn.Close()
}

// Call performs a single RPC request and decodes the result.
func (client *Client) Call(ctx context.Context, method string, params any, result any) error {
	if client == nil {
		return fmt.Errorf("daemon: nil client")
	}
	encodedParams, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("daemon: encode params: %w", err)
	}
	request := Request{ID: randomRequestID(), Token: client.token, Method: method, Params: encodedParams}
	if err := writeFrame(client.conn, request); err != nil {
		return err
	}
	deadline := time.Now().Add(client.timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := client.conn.SetReadDeadline(deadline); err != nil {
		return fmt.Errorf("daemon: set deadline: %w", err)
	}
	frame, err := readFrame(client.reader)
	if err != nil {
		return fmt.Errorf("daemon: read response: %w", err)
	}
	var response Response
	if err := json.Unmarshal(frame, &response); err != nil {
		return fmt.Errorf("daemon: malformed response: %w", err)
	}
	if response.ID != request.ID {
		return fmt.Errorf("daemon: response id mismatch")
	}
	if response.Error != nil {
		switch response.Error.Code {
		case CodeUnauthorized:
			return ErrUnauthorized
		case CodeApprovalDone:
			return ErrApprovalDone
		case CodeApprovalGone:
			return ErrApprovalGone
		default:
			return fmt.Errorf("daemon: %s", response.Error.Message)
		}
	}
	if result != nil {
		if err := json.Unmarshal(response.Result, result); err != nil {
			return fmt.Errorf("daemon: decode result: %w", err)
		}
	}
	return nil
}

func randomRequestID() string {
	id, err := randomID()
	if err != nil {
		return "local"
	}
	return id[:16]
}

var _ = errors.Is
