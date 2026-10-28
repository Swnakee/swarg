package main

import (
	"testing"

	"swarg/internal/tools"
	"swarg/internal/ui"
)

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestJobRows_MapsJobInfo(t *testing.T) {
	tests := []struct {
		name string
		in   []tools.JobInfo
		want []ui.JobRow
	}{
		{name: "empty", in: nil, want: []ui.JobRow{}},
		{
			name: "running",
			in:   []tools.JobInfo{{ID: "bg1", Command: "make", Running: true, ExitCode: 0}},
			want: []ui.JobRow{{ID: "bg1", Command: "make", Running: true, ExitCode: 0}},
		},
		{
			name: "exited with code",
			in:   []tools.JobInfo{{ID: "bg2", Command: "go test", Running: false, ExitCode: 3}},
			want: []ui.JobRow{{ID: "bg2", Command: "go test", Running: false, ExitCode: 3}},
		},
		{
			name: "killed",
			in:   []tools.JobInfo{{ID: "bg3", Command: "sleep 9", Running: false, ExitCode: -1}},
			want: []ui.JobRow{{ID: "bg3", Command: "sleep 9", Running: false, ExitCode: -1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jobRows(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("row %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}
