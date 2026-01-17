package indexing

import (
	"bufio"
	"strings"
)

type Chunk struct {
	Path      string `json:"path"`
	Language  string `json:"language"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	Content   string `json:"content"`
	Score     int    `json:"score,omitempty"`
}

func ChunkFile(path, language, content string, maxLines, maxBytes int) []Chunk {
	if maxLines <= 0 {
		maxLines = 200
	}
	if maxBytes <= 0 {
		maxBytes = 8 * 1024
	}

	sc := bufio.NewScanner(strings.NewReader(content))
	sc.Buffer(make([]byte, 1024), maxBytes*4)

	var chunks []Chunk
	var b strings.Builder
	b.Grow(min(maxBytes, 4096))

	startLine := 1
	lineNo := 0
	linesInChunk := 0

	flush := func(endLine int) {
		txt := strings.TrimRight(b.String(), "\n")
		if strings.TrimSpace(txt) == "" {
			b.Reset()
			linesInChunk = 0
			startLine = endLine + 1
			return
		}
		chunks = append(chunks, Chunk{
			Path:      path,
			Language:  language,
			StartLine: startLine,
			EndLine:   endLine,
			Content:   txt + "\n",
		})
		b.Reset()
		linesInChunk = 0
		startLine = endLine + 1
	}

	for sc.Scan() {
		lineNo++
		line := sc.Text()
		if b.Len()+len(line)+1 > maxBytes || linesInChunk >= maxLines {
			flush(lineNo - 1)
		}
		b.WriteString(line)
		b.WriteByte('\n')
		linesInChunk++
	}
	if b.Len() > 0 {
		flush(lineNo)
	}
	return chunks
}
