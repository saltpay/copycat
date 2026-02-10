package ai

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/saltpay/copycat/internal/config"
)

func TestGeneratePRDescriptionStderr(t *testing.T) {
	// Create a mock AI tool that writes to both stdout and stderr and exits with an error
	aiTool := &config.AITool{
		Name:        "mock-ai",
		Command:     "sh",
		SummaryArgs: []string{"-c", `echo "this is stdout" && echo "this is stderr" >&2; exit 1`},
	}

	project := config.Project{Repo: "test-repo"}
	aiOutput := "some ai output"
	targetPath, _ := os.Getwd()

	_, err := GeneratePRDescription(context.Background(), aiTool, project, aiOutput, targetPath)

	if err == nil {
		t.Fatal("Expected an error but got none")
	}

	// The error message should contain the output from stdout, and not stderr.
	// This validates that cmd.Output() is used, which is smaller than cmd.CombinedOutput().
	expectedErrorPart := "Output: this is stdout"
	if !strings.Contains(err.Error(), expectedErrorPart) {
		t.Errorf("Expected error to contain '%s', but it was: %s", expectedErrorPart, err.Error())
	}

	unexpectedErrorPart := "this is stderr"
	if strings.Contains(err.Error(), unexpectedErrorPart) {
		t.Errorf("Expected error to not contain '%s', but it was: %s", unexpectedErrorPart, err.Error())
	}
}
