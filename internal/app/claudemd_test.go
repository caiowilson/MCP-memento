package app

import "testing"

func TestUpsertClaudeLocalMD(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		block    string
		want     string
	}{
		{
			name:     "creates block when file is empty",
			existing: "",
			block:    claudeMDMarkerStart + "\nBLOCK\n" + claudeMDMarkerEnd + "\n",
			want:     claudeMDMarkerStart + "\nBLOCK\n" + claudeMDMarkerEnd + "\n",
		},
		{
			name:     "appends block with blank line separator when no markers present",
			existing: "Some content\n",
			block:    claudeMDMarkerStart + "\nBLOCK\n" + claudeMDMarkerEnd + "\n",
			want:     "Some content\n\n" + claudeMDMarkerStart + "\nBLOCK\n" + claudeMDMarkerEnd + "\n",
		},
		{
			name:     "replaces block in place when markers already present",
			existing: "Some content\n\n" + claudeMDMarkerStart + "\nOLD BLOCK\n" + claudeMDMarkerEnd + "\n",
			block:    claudeMDMarkerStart + "\nNEW BLOCK\n" + claudeMDMarkerEnd + "\n",
			want:     "Some content\n\n" + claudeMDMarkerStart + "\nNEW BLOCK\n" + claudeMDMarkerEnd + "\n",
		},
		{
			name:     "rerunning with the same block is idempotent",
			existing: "Some content\n\n" + claudeMDMarkerStart + "\nBLOCK\n" + claudeMDMarkerEnd + "\n",
			block:    claudeMDMarkerStart + "\nBLOCK\n" + claudeMDMarkerEnd + "\n",
			want:     "Some content\n\n" + claudeMDMarkerStart + "\nBLOCK\n" + claudeMDMarkerEnd + "\n",
		},
		{
			name:     "preserves content after the end marker",
			existing: claudeMDMarkerStart + "\nOLD BLOCK\n" + claudeMDMarkerEnd + "\nSome trailing note\n",
			block:    claudeMDMarkerStart + "\nNEW BLOCK\n" + claudeMDMarkerEnd + "\n",
			want:     claudeMDMarkerStart + "\nNEW BLOCK\n" + claudeMDMarkerEnd + "\nSome trailing note\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := upsertClaudeLocalMD([]byte(tt.existing), tt.block)
			if string(got) != tt.want {
				t.Errorf("upsertClaudeLocalMD() = %q, want %q", string(got), tt.want)
			}
		})
	}
}
