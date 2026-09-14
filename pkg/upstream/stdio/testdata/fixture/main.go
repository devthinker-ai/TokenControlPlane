// Tiny stdio MCP fixture for ProcessManager tests (echo + slow tools).
// Env:
//
//	CRASH_AFTER=N  — exit after N tools/call completions (0 = never)
//	SLOW_MS=N      — sleep duration for "slow" tool (default 2000)
package main

import (
	"context"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	crashAfter := 0
	if v := os.Getenv("CRASH_AFTER"); v != "" {
		crashAfter, _ = strconv.Atoi(v)
	}
	slowMS := 2000
	if v := os.Getenv("SLOW_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			slowMS = n
		}
	}

	var calls atomic.Int64
	s := server.NewMCPServer("fixture", "1.0.0")
	s.AddTool(mcp.NewTool("echo",
		mcp.WithDescription("echoes input"),
		mcp.WithString("text", mcp.Description("text to echo")),
	), func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		n := calls.Add(1)
		text, _ := req.RequireString("text")
		if crashAfter > 0 && int(n) >= crashAfter {
			go func() {
				time.Sleep(50 * time.Millisecond)
				os.Exit(2)
			}()
		}
		return mcp.NewToolResultText(text), nil
	})
	s.AddTool(mcp.NewTool("slow",
		mcp.WithDescription("sleeps then returns"),
		mcp.WithString("text", mcp.Description("optional text")),
	), func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		n := calls.Add(1)
		time.Sleep(time.Duration(slowMS) * time.Millisecond)
		if crashAfter > 0 && int(n) >= crashAfter {
			os.Exit(2)
		}
		text, _ := req.RequireString("text")
		if text == "" {
			text = "ok"
		}
		return mcp.NewToolResultText(text), nil
	})

	if err := server.ServeStdio(s); err != nil {
		os.Exit(1)
	}
}
