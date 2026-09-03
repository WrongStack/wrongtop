package procs

import (
	"testing"

	"github.com/ersinkoc/wrongtop/internal/collector"
)

func sample() []collector.Proc {
	return []collector.Proc{
		{PID: 100, Name: "Code", User: "ersinkoc", CPU: 12.5, Mem: 8.2},
		{PID: 25, Name: "chrome", User: "ersinkoc", CPU: 6.0, Mem: 4.4},
		{PID: 3000, Name: "sshd", User: "root", CPU: 0.1, Mem: 0.1},
		{PID: 7, Name: "configd", User: "root", CPU: 0.0, Mem: 0.0},
	}
}

func TestFilterNameAndUser(t *testing.T) {
	got := Filter(sample(), "co")
	if len(got) != 2 { // Code + configd
		t.Fatalf("want 2 matches for 'co', got %d", len(got))
	}
	got = Filter(sample(), "ROOT")
	if len(got) != 2 {
		t.Fatalf("user filter case-insensitive: want 2, got %d", len(got))
	}
	got = Filter(sample(), "")
	if len(got) != 4 {
		t.Fatalf("empty filter keeps all: got %d", len(got))
	}
}

func TestFilterNumericPID(t *testing.T) {
	got := Filter(sample(), "30")
	if len(got) != 1 || got[0].PID != 3000 {
		t.Fatalf("numeric filter by pid prefix: got %+v", got)
	}
	got = Filter(sample(), "7")
	if len(got) != 1 || got[0].PID != 7 {
		t.Fatalf("numeric filter '7': got %+v", got)
	}
}

func TestSortOrders(t *testing.T) {
	procs := sample()
	Sort(procs, SortCPU, true) // desc
	if procs[0].Name != "Code" || procs[3].Name != "configd" {
		t.Fatalf("cpu desc: got %s first", procs[0].Name)
	}
	Sort(procs, SortPID, false) // asc
	if procs[0].PID != 7 || procs[3].PID != 3000 {
		t.Fatalf("pid asc: got %d first", procs[0].PID)
	}
	Sort(procs, SortName, false)
	if procs[0].Name != "chrome" {
		t.Fatalf("name asc: got %s first", procs[0].Name)
	}
	Sort(procs, SortUser, true)
	if procs[0].User != "root" {
		t.Fatalf("user desc: got %s first", procs[0].User)
	}
}

func TestSortKeyCycle(t *testing.T) {
	k := SortCPU
	for i, want := range []SortKey{SortMem, SortPID, SortName, SortUser, SortCPU} {
		k = k.Next()
		if k != want {
			t.Fatalf("step %d: want %v, got %v", i+1, want, k)
		}
	}
	if SortCPU.String() != "cpu" || SortUser.String() != "user" {
		t.Fatal("unexpected display names")
	}
}
