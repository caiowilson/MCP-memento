package indexing

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"

	"memento-mcp/internal/embedding"
)

// pendingVectorFiles returns indexed files whose vectors are missing or
// incomplete. The manifest is authoritative, so this costs no file I/O beyond
// confirming the sidecar exists.
func (i *Indexer) pendingVectorFiles() []string {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.embedder == nil {
		return nil
	}
	pending := make([]string, 0)
	for rel, ent := range i.manifest.Files {
		if ent.Chunks == 0 {
			continue
		}
		if ent.Vectors != ent.Chunks {
			pending = append(pending, rel)
			continue
		}
		if _, err := os.Stat(i.vectorFilePath(ent.ID)); err != nil {
			pending = append(pending, rel)
		}
	}
	sort.Strings(pending)
	return pending
}

// pendingVectorCount reports how many indexed files are still awaiting
// vectors. Status uses it to distinguish "warming up" from "broken".
func (i *Indexer) pendingVectorCount() int {
	return len(i.pendingVectorFiles())
}

// backfillVectors embeds chunks that are already persisted. Repairing a vector
// gap must never require re-reading, re-hashing, or re-chunking the source.
func (i *Indexer) backfillVectors(ctx context.Context) error {
	if i.embedder == nil || !i.embeddingRetryReady() {
		return nil
	}
	changed := false
	for _, rel := range i.pendingVectorFiles() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		i.mu.Lock()
		ent, ok := i.manifest.Files[rel]
		i.mu.Unlock()
		if !ok {
			continue
		}
		chunks, err := i.readChunksFile(ent.ID)
		if err != nil || len(chunks) == 0 {
			continue
		}
		vectors, err := i.embedChunks(ctx, chunks)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !errors.Is(err, errEmbeddingBackoff) && !errors.Is(err, embedding.ErrRuntimeUnavailable) {
				i.setError(fmt.Errorf("backfill vectors for %s: %w", rel, err))
			}
			// The runtime is down. Remaining files stay pending for a later pass.
			break
		}
		if len(vectors) == 0 {
			continue
		}

		i.mu.Lock()
		current, stillPresent := i.manifest.Files[rel]
		if stillPresent && current.ID == ent.ID && current.Hash == ent.Hash {
			if writeErr := i.writeVectorsFile(ent.ID, i.embeddingFingerprint(), vectors); writeErr != nil {
				i.removeVectorFile(ent.ID)
				i.status.Error = fmt.Sprintf("persist vectors for %s: %v", rel, writeErr)
			} else {
				current.Vectors = len(vectors)
				i.manifest.Files[rel] = current
				changed = true
			}
		}
		i.mu.Unlock()
	}
	if !changed {
		return nil
	}
	return i.saveManifest()
}
