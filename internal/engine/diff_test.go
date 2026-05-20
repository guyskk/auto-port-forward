package engine

import (
	"reflect"
	"sort"
	"testing"
)

func sortOps(ops []DiffOp) []DiffOp {
	sort.SliceStable(ops, func(i, j int) bool {
		if ops[i].Kind != ops[j].Kind {
			return ops[i].Kind < ops[j].Kind
		}
		return ops[i].Port < ops[j].Port
	})
	return ops
}

func TestDiff_addNewPorts(t *testing.T) {
	got := sortOps(Diff([]int{80, 443}, []int{80, 443, 9527}))
	want := []DiffOp{{Kind: "add", Port: 9527}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestDiff_deleteMissingPorts(t *testing.T) {
	got := sortOps(Diff([]int{80, 443, 9527}, []int{80, 443}))
	want := []DiffOp{{Kind: "del", Port: 9527}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestDiff_noopWhenEqual(t *testing.T) {
	if got := Diff([]int{80, 443}, []int{443, 80}); len(got) != 0 {
		t.Errorf("got %#v, want empty", got)
	}
}

func TestDiff_emptyCurrentMeansAllAdd(t *testing.T) {
	got := sortOps(Diff(nil, []int{80, 443}))
	want := []DiffOp{{Kind: "add", Port: 80}, {Kind: "add", Port: 443}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestDiff_emptyDesiredMeansAllDel(t *testing.T) {
	got := sortOps(Diff([]int{80, 443}, nil))
	want := []DiffOp{{Kind: "del", Port: 80}, {Kind: "del", Port: 443}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestDiff_mixedAddAndDel(t *testing.T) {
	got := sortOps(Diff([]int{80, 443, 5000}, []int{80, 9527}))
	want := []DiffOp{
		{Kind: "add", Port: 9527},
		{Kind: "del", Port: 443},
		{Kind: "del", Port: 5000},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestDiff_duplicatesAreDeduped(t *testing.T) {
	got := sortOps(Diff([]int{80, 80}, []int{80, 80, 443}))
	want := []DiffOp{{Kind: "add", Port: 443}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}
