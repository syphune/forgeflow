package main

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/forgeflow/forgeflow/backend/internal/mcp"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	err := mcp.RunBridge(context.Background(), mcp.BridgeConfig{
		Endpoint:  getenv("FORGEFLOW_MCP_URL", "http://localhost:18080/api/v1/mcp"),
		Token:     strings.TrimSpace(os.Getenv("FORGEFLOW_MCP_TOKEN")),
		ProjectID: strings.TrimSpace(os.Getenv("FORGEFLOW_MCP_PROJECT_ID")),
	})
	if err != nil {
		logger.Error("MCP bridge stopped", "error", err)
		os.Exit(1)
	}
}

func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
