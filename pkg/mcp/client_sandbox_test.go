package mcp

import (
	"context"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/sandbox"
)

func TestCreateTransportWrapsStdioCommandWithSandboxRuntime(t *testing.T) {
	rt := &recordingSandboxRuntime{}
	client := NewMCPClient(&MCPConfig{})
	client.SetSandboxRuntime(rt)

	transport, cmd, err := client.createTransport(context.Background(), MCPServerConfig{
		Name:      "test-server",
		Transport: string(TransportSTDIO),
		Command:   []string{"node", "server.js"},
	})
	if err != nil {
		t.Fatalf("createTransport returned error: %v", err)
	}
	if transport == nil || cmd == nil {
		t.Fatal("expected transport and command")
	}
	if cmd.Path != "sandboxed-node" {
		t.Fatalf("cmd path = %q, want sandboxed-node", cmd.Path)
	}
	if len(cmd.Args) != 3 || cmd.Args[1] != "--wrapped" || cmd.Args[2] != "server.js" {
		t.Fatalf("unexpected command args: %#v", cmd.Args)
	}
	if rt.prepareCount != 1 {
		t.Fatalf("prepare count = %d, want 1", rt.prepareCount)
	}
	if rt.lastReq.Metadata["mcp_server"] != "test-server" {
		t.Fatalf("unexpected sandbox metadata: %#v", rt.lastReq.Metadata)
	}
}

func TestCreateTransportHTTPAliasUsesStreamableTransport(t *testing.T) {
	client := NewMCPClient(&MCPConfig{})

	transport, cmd, err := client.createTransport(context.Background(), MCPServerConfig{
		Name:      "legacy-http",
		Transport: "http",
		URL:       "http://127.0.0.1:3000/mcp",
	})
	if err != nil {
		t.Fatalf("createTransport returned error: %v", err)
	}
	if transport == nil {
		t.Fatal("expected streamable transport for http alias")
	}
	if cmd != nil {
		t.Fatalf("expected no command for http alias, got %#v", cmd)
	}
}

func TestCreateTransportWebsocketStillUnsupported(t *testing.T) {
	client := NewMCPClient(&MCPConfig{})

	_, _, err := client.createTransport(context.Background(), MCPServerConfig{
		Name:      "legacy-ws",
		Transport: "websocket",
		URL:       "ws://127.0.0.1:3000/mcp",
	})
	if err == nil {
		t.Fatal("expected websocket to remain unsupported")
	}
}

type recordingSandboxRuntime struct {
	prepareCount int
	lastReq      sandbox.SandboxRequest
}

func (r *recordingSandboxRuntime) PrepareCommand(_ context.Context, req sandbox.SandboxRequest) (*sandbox.SandboxEnvironment, error) {
	r.prepareCount++
	r.lastReq = req
	return &sandbox.SandboxEnvironment{
		Backend: sandbox.BackendNone,
		Command: "sandboxed-" + req.Command,
		Args:    append([]string{"--wrapped"}, req.Args...),
	}, nil
}

func (r *recordingSandboxRuntime) Cleanup(_ context.Context, _ *sandbox.SandboxEnvironment) error {
	return nil
}
