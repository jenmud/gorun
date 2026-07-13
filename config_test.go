package main

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestConfig_Pipeline(t *testing.T) {
	tests := []struct {
		name    string // description of this test case
		tasks   []Task
		want    []Task
		wantErr bool
	}{
		{
			name: "tasks with no depends_on",
			tasks: []Task{
				{Name: "TaskA"},
				{Name: "TaskB"},
				{Name: "TaskC"},
			},
			want: []Task{
				{Name: "TaskA"},
				{Name: "TaskB"},
				{Name: "TaskC"},
			},
		},
		{
			name: "tasks with simple depends_on",
			tasks: []Task{
				{Name: "TaskA", DependsOn: []string{"TaskB"}},
				{Name: "TaskB"},
				{Name: "TaskC"},
			},
			want: []Task{
				{Name: "TaskB"},
				{Name: "TaskA"},
				{Name: "TaskC"},
			},
		},
		{
			name: "tasks with complicated depends_on",
			tasks: []Task{
				{Name: "TaskA", DependsOn: []string{"TaskB", "TaskC"}},
				{Name: "TaskB"},
				{Name: "TaskC", DependsOn: []string{"TaskB"}},
			},
			want: []Task{
				{Name: "TaskB"},
				{Name: "TaskC"},
				{Name: "TaskA"},
			},
		},
		{
			name: "tasks with cyclic depends_on",
			tasks: []Task{
				{Name: "TaskA", DependsOn: []string{"TaskB", "TaskC"}},
				{Name: "TaskB", DependsOn: []string{"TaskC"}},
				{Name: "TaskC", DependsOn: []string{"TaskB"}},
			},
			want: []Task{
				{Name: "TaskB"},
				{Name: "TaskC"},
				{Name: "TaskA"},
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

func TestConfig_TaskMap(t *testing.T) {
	tests := []struct {
		name  string // description of this test case
		tasks []Task
		want  map[string]Task
	}{
		{
			name: "simple",
			tasks: []Task{
				{Name: "a"},
				{Name: "b"},
				{Name: "c"},
			},
			want: map[string]Task{
				"a": {Name: "a"},
				"b": {Name: "b"},
				"c": {Name: "c"},
			},
		},
		{
			name: "complex",
			tasks: []Task{
				{Name: "a"},
				{Name: "b", DependsOn: []string{"d"}},
				{Name: "c"},
				{Name: "d"},
				{Name: "c"},
			},
			want: map[string]Task{
				"a": {Name: "a"},
				"b": {Name: "b", DependsOn: []string{"d"}},
				"c": {Name: "c"},
				"d": {Name: "d"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c Config
			c.Tasks = tt.tasks

			got := c.TaskMap()

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("%s = %s (- want, + got)", tt.name, diff)
			}
		})
	}
}
