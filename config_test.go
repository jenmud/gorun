package main

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestConfig_Pipeline(t *testing.T) {
	tests := []struct {
		name    string // description of this test case
		tasks   []Task
		want    [][]Task
		wantErr bool
	}{
		{
			name: "tasks with no depends_on",
			tasks: []Task{
				{Name: "TaskA"},
				{Name: "TaskB"},
				{Name: "TaskC"},
			},
			want: [][]Task{
				{
					{Name: "TaskA"},
					{Name: "TaskB"},
					{Name: "TaskC"},
				},
			},
		},
		{
			name: "tasks with simple depends_on",
			tasks: []Task{
				{Name: "TaskA", DependsOn: []string{"TaskB"}},
				{Name: "TaskB"},
				{Name: "TaskC"},
			},
			want: [][]Task{
				{
					{Name: "TaskB"},
					{Name: "TaskA"},
					{Name: "TaskC"},
				},
			},
		},
		{
			name: "tasks with complicated depends_on",
			tasks: []Task{
				{Name: "TaskA", DependsOn: []string{"TaskB", "TaskC"}},
				{Name: "TaskB"},
				{Name: "TaskC", DependsOn: []string{"TaskB"}},
			},
			want: [][]Task{
				{
					{Name: "TaskB"},
					{Name: "TaskC"},
					{Name: "TaskA"},
				},
			},
		},
		{
			name: "tasks with cyclic depends_on",
			tasks: []Task{
				{Name: "TaskA", DependsOn: []string{"TaskB", "TaskC"}},
				{Name: "TaskB", DependsOn: []string{"TaskC"}},
				{Name: "TaskC", DependsOn: []string{"TaskB"}},
			},
			want: [][]Task{
				{
					{Name: "TaskB"},
					{Name: "TaskC"},
					{Name: "TaskA"},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Config{Tasks: tt.tasks}

			got, err := c.Pipeline()

			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error found: %v", err)
			}

			if tt.wantErr && err == nil {
				t.Errorf("expected error but not found: %v", err)
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("%s = %s (- want, + got)", tt.name, diff)
			}
		})
	}
}
