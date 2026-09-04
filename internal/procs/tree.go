package procs

import (
	"cmp"
	"slices"
	"strings"

	"github.com/ersinkoc/wrongtop/internal/collector"
)

// TreeNode is one row of the flattened process forest: a process with
// its ancestry depth and whether it has children in the current set.
type TreeNode struct {
	Proc     collector.Proc
	Depth    int
	Children bool
}

// Less is the comparison a SortKey induces on processes.
func (k SortKey) Less(a, b collector.Proc) int {
	switch k {
	case SortCPU:
		return cmp.Compare(a.CPU, b.CPU)
	case SortMem:
		return cmp.Compare(a.Mem, b.Mem)
	case SortPID:
		return cmp.Compare(a.PID, b.PID)
	case SortName:
		return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	case SortUser:
		return cmp.Compare(a.User, b.User)
	default:
		return 0
	}
}

// sortBy orders a list in place for the tree view (descending by
// default, matching the flat table).
func sortBy(list []collector.Proc, key SortKey, desc bool) {
	slices.SortStableFunc(list, func(a, b collector.Proc) int {
		c := key.Less(a, b)
		if desc {
			c = -c
		}
		return c
	})
}

// BuildTree orders processes depth-first by parentage for the tree view.
// Siblings (and parentless roots) sort with key/desc; processes whose
// parent is not in the set become roots; cycles — possible through PID
// reuse — are cut by emitting unvisited processes afterwards.
// Descendants of PIDs in collapsed are hidden.
func BuildTree(procs []collector.Proc, key SortKey, desc bool, collapsed map[int32]bool) []TreeNode {
	index := make(map[int32]bool, len(procs))
	for _, p := range procs {
		index[p.PID] = true
	}

	byParent := make(map[int32][]collector.Proc, len(procs))
	for _, p := range procs {
		byParent[p.PPID] = append(byParent[p.PPID], p)
	}
	for _, kids := range byParent {
		sortBy(kids, key, desc)
	}

	visited := make(map[int32]bool, len(procs))
	out := make([]TreeNode, 0, len(procs))

	// markSubtree flags a hidden subtree as visited so the cycle
	// fallback below does not resurrect collapsed descendants.
	var markSubtree func(pid int32)
	markSubtree = func(pid int32) {
		for _, kid := range byParent[pid] {
			if visited[kid.PID] {
				continue
			}
			visited[kid.PID] = true
			markSubtree(kid.PID)
		}
	}

	var walk func(pid int32, depth int)
	walk = func(pid int32, depth int) {
		for _, kid := range byParent[pid] {
			if visited[kid.PID] {
				continue
			}
			visited[kid.PID] = true
			out = append(out, TreeNode{
				Proc:     kid,
				Depth:    depth,
				Children: len(byParent[kid.PID]) > 0,
			})
			if collapsed[kid.PID] {
				markSubtree(kid.PID)
				continue
			}
			walk(kid.PID, depth+1)
		}
	}

	// roots: PID 1's children and orphans whose parent left the set
	roots := make([]collector.Proc, 0, 8)
	for _, p := range procs {
		if !index[p.PPID] {
			roots = append(roots, p)
		}
	}
	sortBy(roots, key, desc)
	for _, r := range roots {
		if visited[r.PID] {
			continue
		}
		visited[r.PID] = true
		out = append(out, TreeNode{
			Proc:     r,
			Depth:    0,
			Children: len(byParent[r.PID]) > 0,
		})
		if collapsed[r.PID] {
			markSubtree(r.PID)
			continue
		}
		walk(r.PID, 1)
	}

	// cycles never became roots: emit whatever is left, in input order
	for _, p := range procs {
		if visited[p.PID] {
			continue
		}
		visited[p.PID] = true
		out = append(out, TreeNode{
			Proc:     p,
			Depth:    0,
			Children: len(byParent[p.PID]) > 0,
		})
		if collapsed[p.PID] {
			markSubtree(p.PID)
			continue
		}
		walk(p.PID, 1)
	}
	return out
}
