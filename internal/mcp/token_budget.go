package mcp

const contextTokenEstimator = "utf8_bytes_div_4_ceil"

type contextBudget struct {
	maxTokens  int
	maxBytes   int
	usedTokens int
	usedBytes  int
	clamped    bool
}

func newContextBudget(maxTokens, maxBytes int) *contextBudget {
	return &contextBudget{maxTokens: maxTokens, maxBytes: maxBytes}
}

func estimateTokens(content string) int {
	if content == "" {
		return 0
	}
	return (len(content) + 3) / 4
}

func (b *contextBudget) tryAdd(content string) bool {
	bytes := len(content)
	tokens := estimateTokens(content)
	if exceedsBudget(b.usedTokens, tokens, b.maxTokens) || exceedsBudget(b.usedBytes, bytes, b.maxBytes) {
		b.clamped = true
		return false
	}
	b.usedTokens += tokens
	b.usedBytes += bytes
	return true
}

func (b *contextBudget) limits(maxFiles, maxChunksPerFile int) map[string]any {
	limits := map[string]any{
		"maxFiles":       maxFiles,
		"maxTokens":      b.maxTokens,
		"usedTokens":     b.usedTokens,
		"tokenEstimator": contextTokenEstimator,
		"maxTotalBytes":  b.maxBytes,
		"usedBytes":      b.usedBytes,
		"clamped":        b.clamped,
	}
	if maxChunksPerFile > 0 {
		limits["maxChunksPerFile"] = maxChunksPerFile
	}
	return limits
}

func exceedsBudget(used, additional, maximum int) bool {
	if maximum <= 0 {
		return false
	}
	return additional > maximum-used
}

func compareSignalDensity(signalA, tokensA, signalB, tokensB int) int {
	if tokensA < 1 {
		tokensA = 1
	}
	if tokensB < 1 {
		tokensB = 1
	}
	left := int64(signalA) * int64(tokensB)
	right := int64(signalB) * int64(tokensA)
	if left > right {
		return 1
	}
	if left < right {
		return -1
	}
	if signalA > signalB {
		return 1
	}
	if signalA < signalB {
		return -1
	}
	if tokensA < tokensB {
		return 1
	}
	if tokensA > tokensB {
		return -1
	}
	return 0
}
