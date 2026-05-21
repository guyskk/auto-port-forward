package conflict

import (
	"testing"

	"auto-port-forward/internal/config"
	"auto-port-forward/internal/model"
)

func base(rules config.Rules, in Input) Input {
	in.Rules = rules
	return in
}

func TestClassify_excludedByPortList(t *testing.T) {
	r := config.Rules{ExcludePorts: []int{22, 53}}
	got := Classify(base(r, Input{LocalPort: 22}))
	if got != model.StatusExcluded {
		t.Errorf("got %q, want excluded", got)
	}
}

func TestClassify_excludedByRange(t *testing.T) {
	r := config.Rules{ExcludeRanges: []config.Span{{Lo: 9000, Hi: 9100}}}
	if got := Classify(base(r, Input{LocalPort: 9050})); got != model.StatusExcluded {
		t.Errorf("9050 → %q, want excluded", got)
	}
	if got := Classify(base(r, Input{LocalPort: 9000})); got != model.StatusExcluded {
		t.Errorf("9000 (low) → %q, want excluded", got)
	}
	if got := Classify(base(r, Input{LocalPort: 9100})); got != model.StatusExcluded {
		t.Errorf("9100 (high) → %q, want excluded", got)
	}
	if got := Classify(base(r, Input{LocalPort: 9101})); got == model.StatusExcluded {
		t.Errorf("9101 should not be excluded")
	}
}

func TestClassify_privilegedPortNonRoot(t *testing.T) {
	in := base(config.Rules{}, Input{LocalPort: 80, IsRoot: false})
	if got := Classify(in); got != model.StatusConflictPriv {
		t.Errorf("got %q, want conflict_priv", got)
	}
}

func TestClassify_privilegedPortRoot(t *testing.T) {
	in := base(config.Rules{}, Input{LocalPort: 80, IsRoot: true})
	if got := Classify(in); got == model.StatusConflictPriv {
		t.Errorf("root should not get conflict_priv")
	}
}

func TestClassify_localOccupiedByOther(t *testing.T) {
	in := base(config.Rules{}, Input{
		LocalPort:      8080,
		LocalOccupied:  true,
		OccupiedBySelf: false,
	})
	if got := Classify(in); got != model.StatusConflict {
		t.Errorf("got %q, want conflict", got)
	}
}

func TestClassify_localOccupiedBySelfIsOK(t *testing.T) {
	in := base(config.Rules{}, Input{
		LocalPort:      8080,
		LocalOccupied:  true,
		OccupiedBySelf: true,
	})
	if got := Classify(in); got == model.StatusConflict {
		t.Errorf("self-occupied should not be conflict")
	}
}

func TestClassify_defaultIsPending(t *testing.T) {
	in := base(config.Rules{}, Input{LocalPort: 8080})
	if got := Classify(in); got != model.StatusPending {
		t.Errorf("got %q, want pending", got)
	}
}

func TestClassify_excludePriorityOverConflict(t *testing.T) {
	r := config.Rules{ExcludePorts: []int{80}}
	in := base(r, Input{LocalPort: 80, IsRoot: false, LocalOccupied: true})
	if got := Classify(in); got != model.StatusExcluded {
		t.Errorf("got %q, want excluded (higher priority)", got)
	}
}

func TestClassify_privPriorityOverConflict(t *testing.T) {
	in := base(config.Rules{}, Input{LocalPort: 80, IsRoot: false, LocalOccupied: true})
	if got := Classify(in); got != model.StatusConflictPriv {
		t.Errorf("got %q, want conflict_priv (higher priority than conflict)", got)
	}
}
