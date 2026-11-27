package workflow

// Graph is an ordered representation for inspection and visualization.
type Graph struct {
	Start string      `json:"start"`
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

// GraphNode describes a state, including its retirement and optional origin.
type GraphNode struct {
	ID      string    `json:"id"`
	Retired bool      `json:"retired,omitempty"`
	Patch   PatchInfo `json:"patch,omitzero"`
}

// GraphEdge describes a transition and whether it is effectively retired.
type GraphEdge struct {
	Transition
	Patch PatchInfo `json:"patch,omitzero"`
}

// Graph returns a copy of the current graph in insertion order. Unlike
// Definition, its edge retirement includes retirement of the destination.
func (e *Engine) Graph() Graph {
	result := Graph{Start: e.start, Nodes: make([]GraphNode, 0, len(e.definition.States)), Edges: make([]GraphEdge, 0, len(e.definition.Transitions))}
	for _, id := range e.definition.States {
		result.Nodes = append(result.Nodes, GraphNode{ID: id, Retired: e.retired[id], Patch: e.stateOrigins[id]})
	}
	for _, t := range cloneTransitions(e.definition.Transitions) {
		t.Retired = t.Retired || e.retired[t.To]
		if t.Selection == "" {
			t.Selection = First
		}
		if t.Probability == nil {
			certain := 1.0
			t.Probability = &certain
		}
		result.Edges = append(result.Edges, GraphEdge{Transition: t, Patch: e.origins[t.ID]})
	}
	return result
}
