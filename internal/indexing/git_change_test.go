package indexing

import (
	"testing"
)

func TestParsePorcelainZ(t *testing.T) {
	input := []byte(" M a.go\x00D  b.go\x00R  old.go\x00new.go\x00?? c.go\x00")
	add, del, err := parsePorcelainZ(input)
	if err != nil {
		t.Fatal(err)
	}

	expectAdd := map[string]struct{}{
		"a.go":   {},
		"new.go": {},
		"c.go":   {},
	}
	expectDel := map[string]struct{}{
		"b.go":   {},
		"old.go": {},
	}

	for _, p := range add {
		delete(expectAdd, p)
	}
	for _, p := range del {
		delete(expectDel, p)
	}
	if len(expectAdd) != 0 {
		t.Fatalf("missing add paths: %#v", expectAdd)
	}
	if len(expectDel) != 0 {
		t.Fatalf("missing delete paths: %#v", expectDel)
	}
}
