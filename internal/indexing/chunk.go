package indexing

import (
	"fmt"
	"strings"
)

// ChunkingVersion identifies the persisted chunk-boundary algorithm. Change it
// whenever identical source and limits can produce different ranges.
const ChunkingVersion = "treesitter-v1"

const (
	DefaultMaxChunkLines = 200
	DefaultMaxChunkBytes = 8 * 1024
)

// ChunkingFingerprint returns the persisted identity for an algorithm and its
// effective limits. Zero or negative limits resolve to the production defaults.
func ChunkingFingerprint(maxLines, maxBytes int) string {
	if maxLines <= 0 {
		maxLines = DefaultMaxChunkLines
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxChunkBytes
	}
	return fmt.Sprintf("%s:max-lines=%d:max-bytes=%d", ChunkingVersion, maxLines, maxBytes)
}

type Chunk struct {
	Path      string `json:"path"`
	Language  string `json:"language"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	Content   string `json:"content"`
	Score     int    `json:"score,omitempty"`
}

// ChunkFile chunks content using syntax boundaries when the language supports
// them. Callers that redact content before persistence should use
// chunkFileWithSyntaxSource so parsing can still use the original source.
func ChunkFile(path, language, content string, maxLines, maxBytes int) []Chunk {
	return chunkFileWithSyntaxSource(path, language, content, content, maxLines, maxBytes)
}

func chunkFileWithSyntaxSource(path, language, content, syntaxSource string, maxLines, maxBytes int) []Chunk {
	if maxLines <= 0 {
		maxLines = DefaultMaxChunkLines
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxChunkBytes
	}

	lines := splitChunkLines(content)
	if len(lines) == 0 {
		return nil
	}
	syntaxLines := splitChunkLines(syntaxSource)
	if len(syntaxLines) != len(lines) {
		return chunkLineRange(path, language, lines, 1, len(lines), maxLines, maxBytes)
	}

	starts, ok := syntaxChunkStarts(path, language, syntaxSource, syntaxLines)
	if !ok || len(starts) == 0 {
		return chunkLineRange(path, language, lines, 1, len(lines), maxLines, maxBytes)
	}
	return chunkStructuralRanges(path, language, lines, starts, maxLines, maxBytes)
}

func splitChunkLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func chunkStructuralRanges(path, language string, lines []string, starts []int, maxLines, maxBytes int) []Chunk {
	starts = normalizedChunkStarts(starts, len(lines))
	if len(starts) == 0 {
		return chunkLineRange(path, language, lines, 1, len(lines), maxLines, maxBytes)
	}
	if starts[0] != 1 {
		starts = append([]int{1}, starts...)
	}

	chunks := make([]Chunk, 0, len(starts))
	currentStart, currentEnd := 0, 0
	currentBytes := 0
	flushCurrent := func() {
		if currentStart == 0 {
			return
		}
		if chunk, ok := makeChunk(path, language, lines, currentStart, currentEnd); ok {
			chunks = append(chunks, chunk)
		}
		currentStart, currentEnd, currentBytes = 0, 0, 0
	}

	for index, start := range starts {
		end := len(lines)
		if index+1 < len(starts) {
			end = starts[index+1] - 1
		}
		unitLines := end - start + 1
		unitBytes := chunkRangeBytes(lines, start, end)
		if unitLines > maxLines || unitBytes > maxBytes {
			flushCurrent()
			chunks = append(chunks, chunkLineRange(path, language, lines, start, end, maxLines, maxBytes)...)
			continue
		}
		if currentStart != 0 && (end-currentStart+1 > maxLines || currentBytes+unitBytes > maxBytes) {
			flushCurrent()
		}
		if currentStart == 0 {
			currentStart = start
		}
		currentEnd = end
		currentBytes += unitBytes
	}
	flushCurrent()
	return chunks
}

func chunkLineRange(path, language string, lines []string, start, end, maxLines, maxBytes int) []Chunk {
	chunks := []Chunk{}
	chunkStart := start
	bytes := 0
	for line := start; line <= end; line++ {
		lineBytes := len(lines[line-1]) + 1
		if line > chunkStart && (line-chunkStart >= maxLines || bytes+lineBytes > maxBytes) {
			if chunk, ok := makeChunk(path, language, lines, chunkStart, line-1); ok {
				chunks = append(chunks, chunk)
			}
			chunkStart = line
			bytes = 0
		}
		bytes += lineBytes
	}
	if chunk, ok := makeChunk(path, language, lines, chunkStart, end); ok {
		chunks = append(chunks, chunk)
	}
	return chunks
}

func makeChunk(path, language string, lines []string, start, end int) (Chunk, bool) {
	if start < 1 || end < start || end > len(lines) {
		return Chunk{}, false
	}
	text := strings.TrimRight(strings.Join(lines[start-1:end], "\n")+"\n", "\n")
	if strings.TrimSpace(text) == "" {
		return Chunk{}, false
	}
	return Chunk{
		Path:      path,
		Language:  language,
		StartLine: start,
		EndLine:   end,
		Content:   text + "\n",
	}, true
}

func chunkRangeBytes(lines []string, start, end int) int {
	total := 0
	for line := start; line <= end; line++ {
		total += len(lines[line-1]) + 1
	}
	return total
}

func normalizedChunkStarts(starts []int, lineCount int) []int {
	seen := map[int]struct{}{}
	out := make([]int, 0, len(starts))
	for _, start := range starts {
		if start < 1 || start > lineCount {
			continue
		}
		if _, ok := seen[start]; ok {
			continue
		}
		seen[start] = struct{}{}
		out = append(out, start)
	}
	for index := 1; index < len(out); index++ {
		if out[index] < out[index-1] {
			// Parser and scanner outputs are normally ordered. Sorting this tiny
			// slice keeps the range builder defensive without changing metadata.
			for i := 0; i < len(out); i++ {
				for j := i + 1; j < len(out); j++ {
					if out[j] < out[i] {
						out[i], out[j] = out[j], out[i]
					}
				}
			}
			break
		}
	}
	return out
}
