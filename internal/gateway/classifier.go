package gateway

import (
	"strings"

	openaitypes "github.com/go-llm-gateway/go-llm-gateway/pkg/openai"
)

// routingHint categorises a prompt for automatic provider selection.
type routingHint int

const (
	hintFast     routingHint = iota // Short/conversational → fastest local model.
	hintCode                        // Code/math/reasoning → code-specialised model.
	hintPowerful                    // Long/complex → most capable available model.
)

var codeKeywords = []string{
	"code", "function", "implement", "algorithm", "debug", "error",
	"class", "variable", "syntax", "compile", "runtime", "api", "sql",
	"regex", "python", "javascript", "typescript", "golang", "java",
	"rust", "bash", "script", "loop", "array", "struct", "interface",
	"goroutine", "async", "refactor", "unittest", "fix the", "write a",
}

// classify returns a routing hint based on the last user message.
func classify(msgs []openaitypes.Message) routingHint {
	text := lastUserText(msgs)
	lower := strings.ToLower(text)

	for _, kw := range codeKeywords {
		if strings.Contains(lower, kw) {
			return hintCode
		}
	}

	if estimateTokens(text) > 300 {
		return hintPowerful
	}

	return hintFast
}

// lastUserText returns the content of the last user-role message.
func lastUserText(msgs []openaitypes.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			return msgs[i].Content
		}
	}
	return ""
}

// estimateTokens approximates token count as len(text)/4.
func estimateTokens(text string) int {
	return len(text) / 4
}
