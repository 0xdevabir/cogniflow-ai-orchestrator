package dag

import (
	"fmt"
	"sort"

	"github.com/cogniflow/orchestrator/internal/decomposer"
)

// TopoSort returns node ids in execution levels. len(levels) == max depth + 1.
// levels[i] can run in parallel; levels[i+1] depends only on nodes in levels[0..i].
//
// A node with no dependencies is in level 0. A node that depends on something
// in level k is in level k+1. Cycles & missing deps are errors.
//
// Topological sort is deterministic: within a level, ids are sorted ascending.
func TopoSort(nodes []decomposer.Node, edges []decomposer.Edge) ([][]string, error) {
	if Validate(nodes, edges) != nil {
		return nil, Validate(nodes, edges)
	}

	// Reverse dep map: node id -> set of dependency ids.
	deps := make(map[string][]string, len(nodes))
	for _, n := range nodes {
		// dedup + sort deps for determinism
		seen := map[string]bool{}
		for _, d := range n.DependsOn {
			if !seen[d] {
				seen[d] = true
				deps[n.ID] = append(deps[n.ID], d)
			}
		}
		sort.Strings(deps[n.ID])
	}

	levels := [][]string{}
	assigned := map[string]int{} // node id -> level index

	// A node whose deps are all assigned gets a level one above the max of those.
	for {
		progress := false
		for _, n := range nodes {
			if _, ok := assigned[n.ID]; ok {
				continue
			}
			// Are all deps assigned?
			maxLvl := -1
			ready := true
			for _, d := range deps[n.ID] {
				l, ok := assigned[d]
				if !ok {
					ready = false
					break
				}
				if l > maxLvl {
					maxLvl = l
				}
			}
			if !ready {
				continue
			}
			assigned[n.ID] = maxLvl + 1
			progress = true
		}
		if !progress {
			break
		}
	}

	// Bucket by level.
	maxLvl := -1
	for _, l := range assigned {
		if l > maxLvl {
			maxLvl = l
		}
	}
	for i := 0; i <= maxLvl; i++ {
		levels = append(levels, []string{})
	}
	for id, l := range assigned {
		levels[l] = append(levels[l], id)
	}
	// Sort each level for determinism.
	for i := range levels {
		sort.Strings(levels[i])
	}
	return levels, nil
}

// Validate returns an error if there is a cycle, a dangling edge, or a node with
// an unknown dependency. The error message is human-readable.
func Validate(nodes []decomposer.Node, edges []decomposer.Edge) error {
	known := map[string]bool{}
	for _, n := range nodes {
		if n.ID == "" {
			return fmt.Errorf("dag: node with empty id")
		}
		known[n.ID] = true
	}
	// Check every node's dependencies reference a known node.
	for _, n := range nodes {
		for _, d := range n.DependsOn {
			if !known[d] {
				return fmt.Errorf("dag: node %q has unknown dependency %q", n.ID, d)
			}
			if d == n.ID {
				return fmt.Errorf("dag: node %q depends on itself", n.ID)
			}
		}
	}
	// Check edges for dangling refs.
	for _, e := range edges {
		if !known[e.From] {
			return fmt.Errorf("dag: edge from unknown node %q", e.From)
		}
		if !known[e.To] {
			return fmt.Errorf("dag: edge to unknown node %q", e.To)
		}
	}
	// Cycle detection via DFS over the dependency graph.
	const (
		white = 0 // unvisited
		gray  = 1 // in current DFS stack
		black = 2 // finished
	)
	color := map[string]int{}
	var dfs func(n string, path []string) error
	dfs = func(n string, path []string) error {
		color[n] = gray
		// Find the node to walk its deps.
		var deps []string
		for _, x := range nodes {
			if x.ID == n {
				deps = x.DependsOn
				break
			}
		}
		for _, d := range deps {
			switch color[d] {
			case gray:
				return fmt.Errorf("dag: cycle detected: %v -> %s", append(path, n), d)
			case white:
				if err := dfs(d, append(path, n)); err != nil {
					return err
				}
			}
		}
		color[n] = black
		return nil
	}
	// Run DFS from each node (so we cover all components).
	for _, n := range nodes {
		if color[n.ID] == white {
			if err := dfs(n.ID, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

// Roots returns nodes with no dependencies. Order is by id ascending.
func Roots(nodes []decomposer.Node) []string {
	var out []string
	for _, n := range nodes {
		if len(n.DependsOn) == 0 {
			out = append(out, n.ID)
		}
	}
	sort.Strings(out)
	return out
}
