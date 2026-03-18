package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

type childTransportError struct {
	err error
}

func (e childTransportError) Error() string {
	return e.err.Error()
}

func (e childTransportError) Unwrap() error {
	return e.err
}

func isChildTransportError(err error) bool {
	var target childTransportError
	return errors.As(err, &target)
}

type processChildClient struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner

	mu        sync.Mutex
	waitCh    chan struct{}
	waitErr   error
	nextID    int
	toolDefs  []Tool
	closeOnce sync.Once
}

func (s *Server) spawnProcessChild(root string) (workspaceClient, error) {
	cmd := exec.Command(s.executable, "--root", root, "--child")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if s.devLog {
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stderr = io.Discard
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}

	client := &processChildClient{
		cmd:     cmd,
		stdin:   stdin,
		scanner: bufio.NewScanner(stdout),
		waitCh:  make(chan struct{}),
		nextID:  1,
	}
	client.scanner.Buffer(make([]byte, 1024), 10*1024*1024)

	go func() {
		client.waitErr = cmd.Wait()
		close(client.waitCh)
	}()

	if _, err := client.callRPC("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
	}); err != nil {
		_ = client.Close()
		return nil, err
	}

	rawDefs, err := client.callRPC("tools/list", map[string]any{})
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	var list struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(rawDefs, &list); err != nil {
		_ = client.Close()
		return nil, err
	}
	client.toolDefs = make([]Tool, 0, len(list.Tools))
	for _, def := range list.Tools {
		client.toolDefs = append(client.toolDefs, cloneToolDefinition(def))
	}

	return client, nil
}

func (c *processChildClient) ToolDefinitions(ctx context.Context) ([]Tool, error) {
	_ = ctx
	out := make([]Tool, 0, len(c.toolDefs))
	for _, def := range c.toolDefs {
		out = append(out, cloneToolDefinition(def))
	}
	return out, nil
}

func (c *processChildClient) CallTool(ctx context.Context, name string, args json.RawMessage) (toolCallResult, error) {
	_ = ctx
	if len(args) == 0 {
		args = json.RawMessage([]byte(`{}`))
	}
	raw, err := c.callRPC("tools/call", map[string]any{
		"name":      name,
		"arguments": json.RawMessage(args),
	})
	if err != nil {
		return toolCallResult{}, err
	}

	var res toolCallResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return toolCallResult{}, err
	}
	if res.IsError {
		msg := "child tool call failed"
		if len(res.Content) > 0 && res.Content[0].Text != "" {
			msg = res.Content[0].Text
		}
		return toolCallResult{}, fmt.Errorf("%s", msg)
	}
	return res, nil
}

func (c *processChildClient) Close() error {
	c.closeOnce.Do(func() {
		if c.stdin != nil {
			_ = c.stdin.Close()
		}
		if c.cmd != nil && c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		<-c.waitCh
	})
	return nil
}

func (c *processChildClient) callRPC(method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-c.waitCh:
		return nil, childTransportError{err: fmt.Errorf("child exited: %w", c.waitErr)}
	default:
	}

	paramsRaw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}

	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      c.nextID,
		Method:  method,
		Params:  json.RawMessage(paramsRaw),
	}
	c.nextID++

	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := c.stdin.Write(append(b, '\n')); err != nil {
		return nil, childTransportError{err: err}
	}
	if !c.scanner.Scan() {
		if err := c.scanner.Err(); err != nil {
			return nil, childTransportError{err: err}
		}
		return nil, childTransportError{err: io.EOF}
	}

	var resp rpcResponse
	if err := json.Unmarshal(c.scanner.Bytes(), &resp); err != nil {
		return nil, childTransportError{err: err}
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("%s", resp.Error.Message)
	}
	b, err = json.Marshal(resp.Result)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}
