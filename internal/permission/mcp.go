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

	// Build the HTTP request
	httpReq := permissionHTTPRequest{
		ToolName: args.ToolName,
		Repo:     os.Getenv("COPYCAT_REPO_NAME"),
	}

	// For AskUserQuestion, extract structured question data
	if args.ToolName == "AskUserQuestion" {
		httpReq.Questions = extractQuestions(args.Input)
		httpReq.Command = formatQuestionsForDisplay(httpReq.Questions)
	} else {
		httpReq.Command = extractCommand(args.Input)
	}

	body, _ := json.Marshal(httpReq)

	resp, err := http.Post(baseURL+"/permission", "application/json", bytes.NewReader(body))
	if err != nil {
		return respondDeny(req.ID, "failed to contact permission server")
	}
	defer resp.Body.Close()

	var httpResp permissionHTTPResponse
	if err := json.NewDecoder(resp.Body).Decode(&httpResp); err != nil {
		return respondDeny(req.ID, "failed to decode permission response")
	}

	// For AskUserQuestion, always deny the tool but include the user's answer
	// so Claude can proceed with that information
	if args.ToolName == "AskUserQuestion" && httpResp.Answer != "" {
		return respondDeny(req.ID, fmt.Sprintf("User answered: %s", httpResp.Answer))
	}

	if httpResp.Approved {
		return respondAllow(req.ID, args.Input)
	}
	return respondDeny(req.ID, "User denied permission")
}

// extractQuestions parses the AskUserQuestion input into structured question data.
func extractQuestions(input json.RawMessage) []httpQuestion {
	var obj struct {
		Questions []struct {
			Question string `json:"question"`
			Header   string `json:"header"`
			Options  []struct {
				Label       string `json:"label"`
				Description string `json:"description"`
			} `json:"options"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(input, &obj); err != nil {
		return nil
	}

	var questions []httpQuestion
	for _, q := range obj.Questions {
		hq := httpQuestion{
			Text:   q.Question,
			Header: q.Header,
		}
		for _, o := range q.Options {
			hq.Options = append(hq.Options, httpQuestionOption{
				Label:       o.Label,
				Description: o.Description,
			})
		}
		questions = append(questions, hq)
	}
	return questions
}

// formatQuestionsForDisplay returns a readable string for the Command field fallback.
func formatQuestionsForDisplay(questions []httpQuestion) string {
	if len(questions) == 0 {
		return "AskUserQuestion (no questions)"
	}
	parts := make([]string, 0, len(questions))
	for _, q := range questions {
		parts = append(parts, q.Text)
	}
	return strings.Join(parts, "; ")
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

func respondAllow(id json.RawMessage, updatedInput json.RawMessage) jsonRPCResponse {
	result := map[string]any{
		"content": []map[string]any{
			{
				"type": "text",
				"text": fmt.Sprintf(`{"behavior":"allow","updatedInput":%s}`, updatedInput),
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

func respondDeny(id json.RawMessage, message string) jsonRPCResponse {
	msgJSON, _ := json.Marshal(message)
	result := map[string]any{
		"content": []map[string]any{
			{
				"type": "text",
				"text": fmt.Sprintf(`{"behavior":"deny","message":%s}`, msgJSON),
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
