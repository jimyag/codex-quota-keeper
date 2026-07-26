package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
)

type rpcMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func ReadRateLimits(ctx context.Context, binary string) (RateLimitsResponse, error) {
	cmd := exec.CommandContext(ctx, binary, "app-server")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return RateLimitsResponse{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return RateLimitsResponse{}, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return RateLimitsResponse{}, err
	}

	var stderr cappedBuffer
	stderr.limit = 64 << 10
	stderrDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&stderr, stderrPipe)
		close(stderrDone)
	}()

	if err := cmd.Start(); err != nil {
		return RateLimitsResponse{}, fmt.Errorf("start codex app-server: %w", err)
	}
	defer func() {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		<-stderrDone
	}()

	encoder := json.NewEncoder(stdin)
	if err := encoder.Encode(map[string]any{
		"method": "initialize",
		"id":     1,
		"params": map[string]any{
			"clientInfo": map[string]string{
				"name":    "codex-quota-keeper",
				"title":   "Codex Quota Keeper",
				"version": "0.1.0",
			},
		},
	}); err != nil {
		return RateLimitsResponse{}, fmt.Errorf("initialize app-server: %w", err)
	}
	if err := encoder.Encode(map[string]any{
		"method": "initialized",
		"params": map[string]any{},
	}); err != nil {
		return RateLimitsResponse{}, fmt.Errorf("acknowledge app-server initialization: %w", err)
	}
	if err := encoder.Encode(map[string]any{
		"method": "account/rateLimits/read",
		"id":     2,
		"params": nil,
	}); err != nil {
		return RateLimitsResponse{}, fmt.Errorf("request rate limits: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		var message rpcMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			continue
		}
		if !bytes.Equal(message.ID, []byte("2")) {
			continue
		}
		if message.Error != nil {
			return RateLimitsResponse{}, fmt.Errorf(
				"app-server rate-limit error %d: %s",
				message.Error.Code,
				message.Error.Message,
			)
		}
		var response RateLimitsResponse
		if err := json.Unmarshal(message.Result, &response); err != nil {
			return RateLimitsResponse{}, fmt.Errorf("decode app-server rate limits: %w", err)
		}
		return response, nil
	}
	if err := scanner.Err(); err != nil {
		return RateLimitsResponse{}, fmt.Errorf("read app-server response: %w", err)
	}
	if ctx.Err() != nil {
		return RateLimitsResponse{}, ctx.Err()
	}
	message := stderr.String()
	if message == "" {
		message = "app-server exited without a rate-limit response"
	}
	return RateLimitsResponse{}, errors.New(message)
}

type cappedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *cappedBuffer) Write(data []byte) (int, error) {
	original := len(data)
	if b.buffer.Len() < b.limit {
		remaining := b.limit - b.buffer.Len()
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = b.buffer.Write(data)
	}
	return original, nil
}

func (b *cappedBuffer) String() string {
	return b.buffer.String()
}
