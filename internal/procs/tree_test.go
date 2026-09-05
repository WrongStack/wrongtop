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
