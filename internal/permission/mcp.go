package permission

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// JSON-RPC types for MCP protocol
type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// RunMCPHandler is the entry point for the "permission-handler" subcommand.
// It implements an MCP server over stdin/stdout that bridges permission requests
// to the main copycat process via HTTP.
func RunMCPHandler() error {
	port := os.Getenv("COPYCAT_PERMISSION_PORT")
	if port == "" {
		return fmt.Errorf("COPYCAT_PERMISSION_PORT not set")
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%s", port)

	scanner := bufio.NewScanner(os.Stdin)
	// Increase buffer for large messages
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var req jsonRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}

		// Notifications have no ID — don't respond
		if req.ID == nil || string(req.ID) == "null" {
			continue
		}

		resp := handleMCPRequest(req, baseURL)
		out, err := json.Marshal(resp)
		if err != nil {
			continue
		}
		fmt.Fprintf(os.Stdout, "%s\n", out)
	}

	return scanner.Err()
}

func handleMCPRequest(req jsonRPCRequest, baseURL string) jsonRPCResponse {
	switch req.Method {
	case "initialize":
		return respondResult(req.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]any{
				"name":    "copycat-auth",
				"version": "1.0.0",
			},
		})

	case "tools/list":
		return respondResult(req.ID, map[string]any{
			"tools": []map[string]any{
				{
					"name":        "handle_permission",
					"description": "Handle permission requests for tool execution",
					"inputSchema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"tool_name": map[string]any{
								"type":        "string",
								"description": "The tool requesting permission",
							},
							"input": map[string]any{
								"type":        "object",
								"description": "The tool input/arguments",
							},
						},
						"required": []string{"tool_name", "input"},
					},
				},
			},
		})

	case "tools/call":
		return handleToolCall(req, baseURL)

	default:
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &jsonRPCError{Code: -32601, Message: "method not found"},
		}
	}
}

func handleToolCall(req jsonRPCRequest, baseURL string) jsonRPCResponse {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return respondError(req.ID, -32602, "invalid params")
	}

	var args struct {
		ToolName string          `json:"tool_name"`
		Input    json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(params.Arguments, &args); err != nil {
		return respondError(req.ID, -32602, "invalid arguments")
	}

	// Extract command from input
	command := extractCommand(args.Input)

	// POST to the permission server
	httpReq := permissionHTTPRequest{
		ToolName: args.ToolName,
		Command:  command,
	}
	body, _ := json.Marshal(httpReq)

	resp, err := http.Post(baseURL+"/permission", "application/json", bytes.NewReader(body))
	if err != nil {
		// On error, deny
		return respondToolResult(req.ID, false)
	}
	defer resp.Body.Close()

	var httpResp permissionHTTPResponse
	if err := json.NewDecoder(resp.Body).Decode(&httpResp); err != nil {
		return respondToolResult(req.ID, false)
	}

	return respondToolResult(req.ID, httpResp.Approved)
}

func extractCommand(input json.RawMessage) string {
	// Try to extract "command" field
	var obj map[string]any
	if err := json.Unmarshal(input, &obj); err != nil {
		return string(input)
	}

	if cmd, ok := obj["command"].(string); ok {
		return cmd
	}
	// Fallback: stringify the whole input
	parts := make([]string, 0)
	for k, v := range obj {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	return strings.Join(parts, " ")
}

func respondResult(id json.RawMessage, result any) jsonRPCResponse {
	data, _ := json.Marshal(result)
	return jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  data,
	}
}

func respondError(id json.RawMessage, code int, message string) jsonRPCResponse {
	return jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &jsonRPCError{Code: code, Message: message},
	}
}

func respondToolResult(id json.RawMessage, approved bool) jsonRPCResponse {
	result := map[string]any{
		"content": []map[string]any{
			{
				"type": "text",
				"text": fmt.Sprintf(`{"approved": %v}`, approved),
			},
		},
	}
	data, _ := json.Marshal(result)
	return jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  data,
	}
}
