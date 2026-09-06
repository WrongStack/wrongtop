package procs

import (
	"testing"

	"github.com/wrongstack/wrongtop/internal/collector"
)

func treeProcs() []collector.Proc {
	return []collector.Proc{
		{PID: 20, PPID: 10, Name: "child-a"},
		{PID: 1, PPID: 0, Name: "init"},
		{PID: 30, PPID: 99, Name: "orphan"}, // parent not in set
		{PID: 10, PPID: 1, Name: "parent"},
		{PID: 21, PPID: 10, Name: "child-b"},
	}
}

func names(nodes []TreeNode) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.Proc.Name
	}
	return out
}

func TestBuildTreeDepths(t *testing.T) {
	nodes := BuildTree(treeProcs(), SortPID, false, nil)
	got := names(nodes)
	want := []string{"init", "parent", "child-a", "child-b", "orphan"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	depths := []int{0, 1, 2, 2, 0}
	for i, d := range depths {
		if nodes[i].Depth != d {
			t.Errorf("%s depth = %d, want %d", nodes[i].Proc.Name, nodes[i].Depth, d)
		}
	}
	if !nodes[1].Children {
		t.Error("parent should report children")
	}
	if nodes[4].Children {
		t.Error("orphan should not report children")
	}
}

func TestBuildTreeCollapse(t *testing.T) {
	nodes := BuildTree(treeProcs(), SortPID, false, map[int32]bool{10: true})
	got := names(nodes)
	want := []string{"init", "parent", "orphan"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("collapsed order = %v, want %v", got, want)
		}
	}
}

func TestBuildTreeCycle(t *testing.T) {
	cycle := []collector.Proc{
		{PID: 5, PPID: 6, Name: "a"}, // mutual adoption through PID reuse
		{PID: 6, PPID: 5, Name: "b"},
		{PID: 1, PPID: 0, Name: "init"},
	}
	nodes := BuildTree(cycle, SortPID, false, nil)
	if len(nodes) != 3 {
		t.Fatalf("cycle tree lost rows: %v", names(nodes))
	}
	if nodes[0].Proc.Name != "init" {
		t.Fatalf("init must stay first: %v", names(nodes))
	}
}

func TestBuildTreeSiblingSort(t *testing.T) {
	procs := []collector.Proc{
		{PID: 1, PPID: 0, Name: "init"},
		{PID: 2, PPID: 1, Name: "low-cpu", CPU: 1},
		{PID: 3, PPID: 1, Name: "high-cpu", CPU: 9},
	}
	nodes := BuildTree(procs, SortCPU, true, nil) // descending: busiest sibling first
	if nodes[1].Proc.Name != "high-cpu" {
		t.Fatalf("sibling order = %v, busiest first expected", names(nodes))
	}
}

func TestSortKeyLess(t *testing.T) {
	a := collector.Proc{PID: 1, PPID: 0, Name: "Alpha", User: "ann", CPU: 1, Mem: 2}
	b := collector.Proc{PID: 2, PPID: 0, Name: "beta", User: "bob", CPU: 3, Mem: 4}
	cases := []struct {
		k    SortKey
		want int
	}{
		{SortCPU, -1}, // a.CPU < b.CPU
		{SortMem, -1}, // a.Mem < b.Mem
		{SortPID, -1}, // a.PID < b.PID
		{SortName, -1},
		{SortUser, -1}, // ann < bob
		{SortKey(42), 0},
	}
	for _, c := range cases {
		if got := sign(c.k.Less(a, b)); got != c.want {
			t.Errorf("SortKey(%d).Less(a, b) sign = %d, want %d", c.k, got, c.want)
		}
		if got := sign(c.k.Less(b, a)); got != -c.want {
			t.Errorf("SortKey(%d).Less(b, a) sign = %d, want %d", c.k, got, -c.want)
		}
	}
	// name comparison is case-insensitive: "Alpha" sorts beside "ALPHA"
	loud := a
	loud.Name = "ALPHA"
	if got := SortName.Less(a, loud); got != 0 {
		t.Errorf("name Less should fold case, got %d", got)
	}
	// equal values compare equal
	if got := SortCPU.Less(a, a); got != 0 {
		t.Errorf("Less(a, a) = %d, want 0", got)
	}
}

func sign(v int) int {
	switch {
	case v < 0:
		return -1
	case v > 0:
		return 1
	default:
		return 0
	}
}

// TestBuildTreeCollapsedRoot covers collapsing at depth 0: the whole
// subtree disappears behind the root row.
func TestBuildTreeCollapsedRoot(t *testing.T) {
	nodes := BuildTree(treeProcs(), SortPID, false, map[int32]bool{1: true})
	got := names(nodes)
	want := []string{"init", "orphan"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("collapsed-root order = %v, want %v", got, want)
		}
	}
	if !nodes[0].Children {
		t.Error("collapsed root keeps its Children flag (the kids are hidden, not gone)")
	}
}

// TestBuildTreeCollapseCycleFallback hides one half of a PID-reuse
// cycle: neither member is a root, so the fallback pass emits the
// first and the collapse markSubtree meets its own tail again.
func TestBuildTreeCollapseCycleFallback(t *testing.T) {
	cycle := []collector.Proc{
		{PID: 2, PPID: 3, Name: "a"},
		{PID: 3, PPID: 2, Name: "b"},
	}
	nodes := BuildTree(cycle, SortPID, false, map[int32]bool{2: true})
	got := names(nodes)
	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("fallback collapsed cycle = %v, want [a]", got)
	}
}

// TestBuildTreeDuplicatePIDRoots covers the root-loop guard for a pid
// that appears twice in the table (PID reuse): the duplicate is already
// visited and must be skipped.
func TestBuildTreeDuplicatePIDRoots(t *testing.T) {
	dup := []collector.Proc{
		{PID: 7, PPID: 0, Name: "one"},
		{PID: 7, PPID: 0, Name: "one-again"}, // same pid, e.g. reused between scans
	}
	nodes := BuildTree(dup, SortPID, false, nil)
	got := names(nodes)
	if len(got) != 1 || got[0] != "one" {
		t.Fatalf("duplicate-pid tree = %v, want [one]", got)
	}
}
