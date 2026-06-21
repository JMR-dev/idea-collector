package ratelimit

import "testing"

func TestAllowConsumesBurstThenBlocks(t *testing.T) {
	l := New(60, 3) // 1/sec, burst 3
	defer l.Close()

	for i := 0; i < 3; i++ {
		if !l.Allow("1.2.3.4") {
			t.Fatalf("request %d within burst should be allowed", i)
		}
	}
	if l.Allow("1.2.3.4") {
		t.Errorf("4th immediate request should be blocked")
	}
}

func TestAllowIsPerKey(t *testing.T) {
	l := New(60, 1)
	defer l.Close()

	if !l.Allow("a") {
		t.Fatal("first key-a request should be allowed")
	}
	if !l.Allow("b") {
		t.Fatal("first key-b request should be allowed (independent bucket)")
	}
	if l.Allow("a") {
		t.Error("second key-a request should be blocked")
	}
}
