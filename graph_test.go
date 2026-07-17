package main

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestGraph_Node(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for receiver constructor.
		taskName string
		task     []Task
		want     Node
		wantErr  bool
	}{
		{
			name:     "empty graph",
			taskName: "some-task",
			task:     []Task{},
			want:     Node{},
			wantErr:  true,
		},
		{
			name:     "multiple tasks",
			taskName: "b",
			task: []Task{
				{Name: "c"},
				{Name: "a"},
				{Name: "b"},
			},
			want: Node{
				Task: Task{
					Name: "b",
				},
				Inbound:  []Node{},
				Outbound: []Node{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, err := NewGraph(tt.task...)
			if err != nil {
				t.Fatalf("could not construct receiver type: %v", err)
			}

			got, gotErr := g.Node(tt.taskName)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("%s failed: %v", tt.name, gotErr)
				}
				return
			}

			if tt.wantErr {
				t.Errorf("%s expected error but got nil", tt.name)
				return
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("%s = %s (- want, + got)", tt.name, diff)
			}
		})
	}
}

func TestGraph_Nodes(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for receiver constructor.
		task []Task
		want []Node
	}{
		{
			name: "empty graph",
			task: []Task{},
			want: []Node{},
		},
		{
			name: "multiple tasks with no depends on",
			task: []Task{
				{Name: "c"},
				{Name: "a"},
				{Name: "b"},
			},
			want: []Node{
				{
					Task:     Task{Name: "a"},
					Inbound:  []Node{},
					Outbound: []Node{},
				},
				{
					Task:     Task{Name: "b"},
					Inbound:  []Node{},
					Outbound: []Node{},
				},
				{
					Task:     Task{Name: "c"},
					Inbound:  []Node{},
					Outbound: []Node{},
				},
			},
		},
		{
			name: "multiple tasks with depends on",
			task: []Task{
				{Name: "c"},
				{Name: "a", DependsOn: []string{"b"}},
				{Name: "b"},
			},
			want: []Node{
				{
					Task: Task{Name: "a", DependsOn: []string{"b"}},
					Inbound: []Node{
						{
							Task:    Task{Name: "b"},
							Inbound: []Node{},
							Outbound: []Node{
								{
									Task:     Task{Name: "a", DependsOn: []string{"b"}},
									Inbound:  []Node{},
									Outbound: []Node{},
								},
							},
						},
					},
					Outbound: []Node{},
				},
				{
					Task:    Task{Name: "b"},
					Inbound: []Node{},
					Outbound: []Node{
						{
							Task:     Task{Name: "a", DependsOn: []string{"b"}},
							Inbound:  []Node{},
							Outbound: []Node{},
						},
					},
				},
				{
					Task:     Task{Name: "c"},
					Inbound:  []Node{},
					Outbound: []Node{},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, err := NewGraph(tt.task...)
			if err != nil {
				t.Fatalf("could not construct receiver type: %v", err)
			}

			got := g.Nodes()
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("%s = %s (- want, + got)", tt.name, diff)
			}
		})
	}
}

func TestGraph_Ordered(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for receiver constructor.
		task    []Task
		want    []Task
		wantErr bool
	}{
		{
			name:    "empty graph",
			task:    []Task{},
			want:    []Task{},
			wantErr: false,
		},
		{
			name: "multiple tasks with no depends on",
			task: []Task{
				{Name: "a"},
				{Name: "b"},
				{Name: "c"},
			},
			want: []Task{
				{Name: "a"},
				{Name: "b"},
				{Name: "c"},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, err := NewGraph(tt.task...)
			if err != nil {
				t.Fatalf("could not construct receiver type: %v", err)
			}

			got, gotErr := g.Ordered()
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("Ordered() failed: %v", gotErr)
				}
				return
			}

			if tt.wantErr {
				t.Fatal("Ordered() succeeded unexpectedly")
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("%s = %s (- want, + got)", tt.name, diff)
			}
		})
	}
}
