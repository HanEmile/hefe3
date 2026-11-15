package store

import (
	"errors"
	"testing"
	"time"
)

func TestMagicLinkRoundTrip(t *testing.T) {
	st := newTestStore(t)
	user := mustCreateUser(t, st, "alice")

	hash := "deadbeef"
	if err := st.CreateMagicLink(user.ID, hash, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateMagicLink() error = %v", err)
	}

	gotID, err := st.ConsumeMagicLink(hash)
	if err != nil {
		t.Fatalf("ConsumeMagicLink() error = %v", err)
	}
	if gotID != user.ID {
		t.Fatalf("ConsumeMagicLink() = %d, want %d", gotID, user.ID)
	}
}

// TestMagicLinkSingleUse guards the point of the feature: a login link must
// stop working the instant it's been clicked once.
func TestMagicLinkSingleUse(t *testing.T) {
	st := newTestStore(t)
	user := mustCreateUser(t, st, "alice")

	hash := "deadbeef"
	if err := st.CreateMagicLink(user.ID, hash, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateMagicLink() error = %v", err)
	}
	if _, err := st.ConsumeMagicLink(hash); err != nil {
		t.Fatalf("first ConsumeMagicLink() error = %v", err)
	}

	if _, err := st.ConsumeMagicLink(hash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second ConsumeMagicLink() error = %v, want ErrNotFound", err)
	}
}

func TestMagicLinkExpired(t *testing.T) {
	st := newTestStore(t)
	user := mustCreateUser(t, st, "alice")

	hash := "deadbeef"
	if err := st.CreateMagicLink(user.ID, hash, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("CreateMagicLink() error = %v", err)
	}

	if _, err := st.ConsumeMagicLink(hash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ConsumeMagicLink(expired) error = %v, want ErrNotFound", err)
	}
}

func TestMagicLinkUnknownToken(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.ConsumeMagicLink("never-issued"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ConsumeMagicLink(unknown) error = %v, want ErrNotFound", err)
	}
}

func TestPruneExpiredMagicLinks(t *testing.T) {
	st := newTestStore(t)
	user := mustCreateUser(t, st, "alice")

	if err := st.CreateMagicLink(user.ID, "expired", time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("CreateMagicLink(expired) error = %v", err)
	}
	if err := st.CreateMagicLink(user.ID, "fresh", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateMagicLink(fresh) error = %v", err)
	}

	if err := st.PruneExpiredMagicLinks(); err != nil {
		t.Fatalf("PruneExpiredMagicLinks() error = %v", err)
	}

	if _, err := st.ConsumeMagicLink("expired"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ConsumeMagicLink(pruned) error = %v, want ErrNotFound", err)
	}
	if _, err := st.ConsumeMagicLink("fresh"); err != nil {
		t.Fatalf("ConsumeMagicLink(fresh, after prune) error = %v, want nil", err)
	}
}
