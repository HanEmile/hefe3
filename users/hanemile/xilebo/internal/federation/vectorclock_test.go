package federation

import "testing"

func TestVectorClockIncrement(t *testing.T) {
	vc := NewVectorClock()
	vc.Increment("server-a")
	vc.Increment("server-a")
	vc.Increment("server-b")

	if vc.Get("server-a") != 2 {
		t.Errorf("server-a = %d, want 2", vc.Get("server-a"))
	}
	if vc.Get("server-b") != 1 {
		t.Errorf("server-b = %d, want 1", vc.Get("server-b"))
	}
	if vc.Get("server-c") != 0 {
		t.Errorf("server-c = %d, want 0", vc.Get("server-c"))
	}
}

func TestVectorClockMerge(t *testing.T) {
	a := VectorClock{"s1": 3, "s2": 1}
	b := VectorClock{"s1": 2, "s2": 4, "s3": 1}

	merged := a.Merge(b)

	if merged["s1"] != 3 {
		t.Errorf("s1 = %d, want 3", merged["s1"])
	}
	if merged["s2"] != 4 {
		t.Errorf("s2 = %d, want 4", merged["s2"])
	}
	if merged["s3"] != 1 {
		t.Errorf("s3 = %d, want 1", merged["s3"])
	}
}

func TestVectorClockHappensBefore(t *testing.T) {
	a := VectorClock{"s1": 1, "s2": 2}
	b := VectorClock{"s1": 2, "s2": 3}

	if !a.HappensBefore(b) {
		t.Error("expected a < b")
	}
	if b.HappensBefore(a) {
		t.Error("expected b NOT < a")
	}
}

func TestVectorClockConcurrent(t *testing.T) {
	a := VectorClock{"s1": 3, "s2": 1}
	b := VectorClock{"s1": 2, "s2": 4}

	if !a.Concurrent(b) {
		t.Error("expected concurrent")
	}
	if a.HappensBefore(b) {
		t.Error("should not happen-before")
	}
	if b.HappensBefore(a) {
		t.Error("should not happen-before")
	}
}

func TestVectorClockParse(t *testing.T) {
	vc, err := ParseVectorClock(`{"server-a":3,"server-b":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if vc["server-a"] != 3 {
		t.Errorf("server-a = %d, want 3", vc["server-a"])
	}

	vc2, err := ParseVectorClock("")
	if err != nil {
		t.Fatal(err)
	}
	if len(vc2) != 0 {
		t.Error("expected empty clock from empty string")
	}
}
