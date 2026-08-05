package ratelimit

import (
	"sync"
	"testing"
	"time"
)

func TestStore_AllowsWithinLimit(t *testing.T) {
	s := NewStore(2, 15*time.Minute)
	for i := 0; i < 2; i++ {
		if ok, retry := s.Allow("ip:1.2.3.4"); !ok {
			t.Fatalf("attempt %d: expected allowed, got blocked (retry %v)", i+1, retry)
		}
	}
}

func TestStore_BlocksOverLimit(t *testing.T) {
	s := NewStore(2, 15*time.Minute)
	s.Allow("ip:1.2.3.4")
	s.Allow("ip:1.2.3.4")

	ok, retry := s.Allow("ip:1.2.3.4")
	if ok {
		t.Fatal("third attempt: expected blocked")
	}
	if retry <= 0 || retry > 15*time.Minute {
		t.Errorf("retryAfter = %v, want within (0, 15m]", retry)
	}
}

func TestStore_IndependentKeys(t *testing.T) {
	s := NewStore(1, 15*time.Minute)
	if ok, _ := s.Allow("ip:1.1.1.1"); !ok {
		t.Fatal("first key should be allowed")
	}
	// A different key is not affected by the first key's limit.
	if ok, _ := s.Allow("ip:2.2.2.2"); !ok {
		t.Fatal("second key should be allowed independently")
	}
	if ok, _ := s.Allow("ip:1.1.1.1"); ok {
		t.Fatal("first key should now be over its limit")
	}
}

func TestStore_WindowExpiry(t *testing.T) {
	s := NewStore(1, 50*time.Millisecond)

	if ok, _ := s.Allow("k"); !ok {
		t.Fatal("first attempt should be allowed")
	}
	if ok, _ := s.Allow("k"); ok {
		t.Fatal("second attempt should be blocked")
	}

	time.Sleep(120 * time.Millisecond) // comfortably past the 50ms window (CI-safe)
	if ok, _ := s.Allow("k"); !ok {
		t.Fatal("after the window passes, attempts should be allowed again")
	}
}

func TestStore_Concurrent(t *testing.T) {
	s := NewStore(5, 15*time.Minute)

	const goroutines = 20
	var wg sync.WaitGroup
	allowed := make([]bool, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ok, _ := s.Allow("ip:9.9.9.9")
			allowed[i] = ok
		}(i)
	}
	wg.Wait()

	okCount := 0
	for _, a := range allowed {
		if a {
			okCount++
		}
	}
	if okCount != 5 {
		t.Errorf("allowed = %d, want exactly 5 (limit)", okCount)
	}
}

func TestLimiter_LoginCombinesIPAndEmail(t *testing.T) {
	// IP limit 3, email limit 2 — the tighter constraint must win.
	l := NewWithLimits(3, 2, 3, 2, 3, 15*time.Minute)

	// Two attempts on the same email from different IPs: both pass IP limits,
	// then the email limit blocks the third.
	if ok, _ := l.LoginAllowed("10.0.0.1", "alice@example.com"); !ok {
		t.Fatal("attempt 1 should be allowed")
	}
	if ok, _ := l.LoginAllowed("10.0.0.2", "alice@example.com"); !ok {
		t.Fatal("attempt 2 should be allowed")
	}
	if ok, _ := l.LoginAllowed("10.0.0.3", "alice@example.com"); ok {
		t.Fatal("attempt 3 (same email) should be blocked by the email limit")
	}

	// A different email from a fresh IP is unaffected.
	if ok, _ := l.LoginAllowed("10.0.0.4", "bob@example.com"); !ok {
		t.Fatal("different email should be allowed")
	}
}

func TestLimiter_ResetIsPerIPOnly(t *testing.T) {
	l := NewWithLimits(10, 5, 10, 5, 1, 15*time.Minute)

	if ok, _ := l.ResetAllowed("10.0.0.1"); !ok {
		t.Fatal("first reset attempt should be allowed")
	}
	if ok, _ := l.ResetAllowed("10.0.0.1"); ok {
		t.Fatal("second reset attempt from same IP should be blocked")
	}
	if ok, _ := l.ResetAllowed("10.0.0.2"); !ok {
		t.Fatal("different IP should be allowed")
	}
}
