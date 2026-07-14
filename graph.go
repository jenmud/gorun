package main

import (
	"fmt"
)

type Node struct {
	Task     Task
	Inbound  []Node
	Outbound []Node
}

// newNode creates a new node from that given task.
func newNode(t Task) Node {
	return Node{Task: t, Inbound: []Node{}, Outbound: []Node{}}
}

// Graph is a execution graph.
type Graph struct {
	nodes map[string]Node
}

// NewGraph returns a new graph with the given tasks.
func NewGraph(task ...Task) (*Graph, error) {
	g := &Graph{nodes: make(map[string]Node)}

	// add in all the nodes to the graph
	for _, t := range task {
		if _, ok := g.nodes[t.Name]; !ok {
			g.nodes[t.Name] = newNode(t)
		}
	}

	// now we will run over all the tasks and link them up.
	for _, node := range g.nodes {
		for _, dep := range node.Task.DependsOn {
			d, ok := g.nodes[dep]
			if !ok {
				return nil, fmt.Errorf("depends_on task %q not found", dep)
			}

			d.Outbound = append(d.Outbound, node)
			node.Inbound = append(node.Inbound, d)

			// update the nodes
			g.nodes[d.Task.Name] = d
			g.nodes[node.Task.Name] = node
		}
	}

	return g, nil
}

// Node will return a node if found.
func (g *Graph) Node(name string) (Node, error) {
	n, ok := g.nodes[name]
	if !ok {
		return Node{}, fmt.Errorf("node task %q not found", name)
	}
	return n, nil
}

// Nodes will return all known nodes.
func (g *Graph) Nodes() []Node {
	nodes := make([]Node, len(g.nodes))

	i := 0
	for _, node := range g.nodes {
		nodes[i] = node
		i++
	}

	return nodes
}
