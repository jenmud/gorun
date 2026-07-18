package main

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestTopologicalSort(t *testing.T) {
	tests := []struct {
		name    string // description of this test case
		tasks   []Task
		want    []Task
		wantErr bool
	}{
		{
			name:  "empty",
			tasks: []Task{},
			want:  []Task{},
		},
		{
			name: "tasks with no depends on",
			tasks: []Task{
				{Name: "taskA"},
				{Name: "taskB"},
				{Name: "taskC"},
			},
			want: []Task{
				{Name: "taskA"},
				{Name: "taskB"},
				{Name: "taskC"},
			},
		},
		{
			name: "a depends on b",
			tasks: []Task{
				{Name: "taskA", DependsOn: []string{"taskB"}},
				{Name: "taskB"},
				{Name: "taskC"},
			},
			want: []Task{
				{Name: "taskB"},
				{Name: "taskC"},
				{Name: "taskA", DependsOn: []string{"taskB"}},
			},
		},
		{
			name: "a depends on b, and c",
			tasks: []Task{
				{Name: "taskA", DependsOn: []string{"taskB", "taskC"}},
				{Name: "taskB"},
				{Name: "taskC"},
			},
			want: []Task{
				{Name: "taskB"},
				{Name: "taskC"},
				{Name: "taskA", DependsOn: []string{"taskB", "taskC"}},
			},
		},
		{
			name: "a depends on b and b depends on c",
			tasks: []Task{
				{Name: "taskA", DependsOn: []string{"taskB"}},
				{Name: "taskB", DependsOn: []string{"taskC"}},
				{Name: "taskC"},
			},
			want: []Task{
				{Name: "taskC"},
				{Name: "taskB", DependsOn: []string{"taskC"}},
				{Name: "taskA", DependsOn: []string{"taskB"}},
			},
		},
		{
			name: "a depends on missing task",
			tasks: []Task{
				{Name: "taskA", DependsOn: []string{"some-missing-task"}},
				{Name: "taskB"},
				{Name: "taskC"},
			},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TopologicalSort(tt.tasks...)
			if err != nil {
				if !tt.wantErr {
					t.Errorf("%s: did not expect error but got: %v", tt.name, err)
				}
				return
			}

			if tt.wantErr {
				t.Errorf("%s: expected error but got nil", tt.name)
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("%s: %s (want -, got +)", tt.name, diff)
			}
		})
	}
}
