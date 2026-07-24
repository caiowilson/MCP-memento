package indexing

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

type trackingRelationshipProvider struct {
	fingerprint   string
	invalidations int
}

func (provider *trackingRelationshipProvider) Fingerprint() string {
	return provider.fingerprint
}

func (*trackingRelationshipProvider) Relationships(context.Context, []string) ([]RelationshipEdge, error) {
	return nil, nil
}

func (provider *trackingRelationshipProvider) InvalidateRelationships() {
	provider.invalidations++
}

func TestRelationshipProviderFingerprintAndIndexInvalidation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Provider.php"), []byte("<?php final class Provider {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	provider := &trackingRelationshipProvider{fingerprint: "test-relationships-v1"}
	idx, err := New(Config{RootAbs: root, StoreDir: t.TempDir(), RelationshipProvider: provider})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	idx.Start(ctx)
	if err := idx.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}
	if provider.invalidations != 1 {
		t.Fatalf("full index invalidations = %d, want 1", provider.invalidations)
	}
	if got, want := idx.DebugInfo().TermSearchVersion, "terms-v15+test-relationships-v1"; got != want {
		t.Fatalf("term search adapter = %q, want %q", got, want)
	}
	if err := idx.Clear(); err != nil {
		t.Fatal(err)
	}
	if provider.invalidations != 2 {
		t.Fatalf("clear invalidations = %d, want 2", provider.invalidations)
	}
}

func TestRelationshipProviderRequiresFingerprint(t *testing.T) {
	_, err := New(Config{
		RootAbs:              t.TempDir(),
		StoreDir:             t.TempDir(),
		RelationshipProvider: &trackingRelationshipProvider{},
	})
	if err == nil {
		t.Fatal("expected an empty relationship-provider fingerprint to fail")
	}
}
