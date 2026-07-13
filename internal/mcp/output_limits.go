package mcp

const (
	defaultAnthropicMaxResultSizeChars = 500_000

	defaultRepoContextMaxTokens      = 7_000
	defaultRepoContextMaxTotalBytes  = 32_000
	defaultRepoDiffContextMaxTokens  = 4_000
	defaultRepoDiffContextMaxBytes   = 16_000
	defaultRepoDiffContextMaxPaths   = 20
	defaultRepoDiffContextMaxChunks  = 3
	defaultRepoDiffContextDiffBytes  = 12_000
	maximumRepoDiffContextDiffBytes  = 64_000
	defaultRepoDiffContextDiffLines  = 3
	maximumRepoDiffContextDiffLines  = 10
	defaultRepoReadFileMaxBytes      = 32_000
	defaultRepoSearchMaxSnippetBytes = 500

	truncationMarker = "..."
)

func largeResultToolMeta() map[string]any {
	return map[string]any{
		"anthropic/maxResultSizeChars": defaultAnthropicMaxResultSizeChars,
	}
}

func truncateStringBytes(s string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s, false
	}
	if maxBytes <= len(truncationMarker) {
		return truncationMarker[:maxBytes], true
	}
	prefix := prefixStringBytes(s, maxBytes-len(truncationMarker))
	if prefix == "" {
		return truncationMarker, true
	}
	return prefix + truncationMarker, true
}

func truncateStringAroundBytes(s string, matchStart, matchLen, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s, false
	}
	if matchStart < 0 || matchStart > len(s) {
		return truncateStringBytes(s, maxBytes)
	}
	if matchLen < 0 {
		matchLen = 0
	}
	matchEnd := matchStart + matchLen
	if matchEnd < matchStart {
		matchEnd = matchStart
	}
	if matchEnd > len(s) {
		matchEnd = len(s)
	}

	if maxBytes <= len(truncationMarker) {
		return truncationMarker[:maxBytes], true
	}

	start, end := truncationWindow(len(s), matchStart, matchEnd, maxBytes-2*len(truncationMarker))
	for i := 0; i < 4; i++ {
		markerBytes := 0
		if start > 0 {
			markerBytes += len(truncationMarker)
		}
		if end < len(s) {
			markerBytes += len(truncationMarker)
		}

		nextStart, nextEnd := truncationWindow(len(s), matchStart, matchEnd, maxBytes-markerBytes)
		if nextStart == start && nextEnd == end {
			break
		}
		start, end = nextStart, nextEnd
	}

	start = stringByteBoundaryAtOrAfter(s, start)
	end = stringByteBoundaryAtOrBefore(s, end)
	if end < start {
		end = start
	}

	out := s[start:end]
	if start > 0 {
		out = truncationMarker + out
	}
	if end < len(s) {
		out += truncationMarker
	}
	if len(out) > maxBytes {
		return truncateStringBytes(out, maxBytes)
	}
	return out, true
}

func truncationWindow(totalBytes, matchStart, matchEnd, budget int) (int, int) {
	if totalBytes <= 0 || budget <= 0 {
		return 0, 0
	}
	if budget >= totalBytes {
		return 0, totalBytes
	}

	if matchStart < 0 {
		matchStart = 0
	}
	if matchStart > totalBytes {
		matchStart = totalBytes
	}
	if matchEnd < matchStart {
		matchEnd = matchStart
	}
	if matchEnd > totalBytes {
		matchEnd = totalBytes
	}

	matchBytes := matchEnd - matchStart
	if matchBytes >= budget {
		end := matchStart + budget
		if end > totalBytes {
			end = totalBytes
			matchStart = end - budget
		}
		return matchStart, end
	}

	before := (budget - matchBytes) / 2
	start := matchStart - before
	if start < 0 {
		start = 0
	}
	end := start + budget
	if end < matchEnd {
		end = matchEnd
		start = end - budget
	}
	if end > totalBytes {
		end = totalBytes
		start = end - budget
	}
	if start < 0 {
		start = 0
	}
	return start, end
}

func prefixStringBytes(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	end := 0
	for i := range s {
		if i > maxBytes {
			break
		}
		end = i
	}
	return s[:end]
}

func stringByteBoundaryAtOrBefore(s string, maxBytes int) int {
	if maxBytes <= 0 {
		return 0
	}
	if len(s) <= maxBytes {
		return len(s)
	}
	end := 0
	for i := range s {
		if i > maxBytes {
			break
		}
		end = i
	}
	return end
}

func stringByteBoundaryAtOrAfter(s string, minBytes int) int {
	if minBytes <= 0 {
		return 0
	}
	if minBytes >= len(s) {
		return len(s)
	}
	for i := range s {
		if i >= minBytes {
			return i
		}
	}
	return len(s)
}
