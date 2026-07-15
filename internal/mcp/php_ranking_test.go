package mcp

import (
	"context"
	"reflect"
	"testing"

	"memento-mcp/internal/indexing"
)

func TestPHPRelationshipProviderReturnsCandidateEdgesOnce(t *testing.T) {
	root := t.TempDir()
	writePHPFrameworkFixture(t, root, "composer.json", `{
  "autoload": {
    "psr-4": {"Fixture\\\\": "src/"}
  }
}`)
	writePHPFrameworkFixture(t, root, "src/Provider.php", `<?php
namespace Fixture;

final class Provider {}
`)
	writePHPFrameworkFixture(t, root, "src/Consumer.php", `<?php
namespace Fixture;

use Fixture\Provider;

final class Consumer
{
    public function __construct(private Provider $provider) {}
}
`)

	provider := NewPHPRelationshipProvider(root)
	edges, err := provider.Relationships(context.Background(), []string{
		"src/./Consumer.php",
		"src/Provider.php",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []indexing.RelationshipEdge{{
		FromPath: "src/Consumer.php",
		ToPath:   "src/Provider.php",
	}}
	if !reflect.DeepEqual(edges, want) {
		t.Fatalf("candidate relationship edges = %#v, want %#v", edges, want)
	}

	filtered, err := provider.Relationships(context.Background(), []string{"src/Consumer.php"})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 0 {
		t.Fatalf("provider introduced an edge outside the candidate set: %#v", filtered)
	}
}
