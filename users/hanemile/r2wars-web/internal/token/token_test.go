package token

import "testing"

func TestNewProducesUniqueTokensWithMatchingHash(t *testing.T) {
	raw1, hash1, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	raw2, hash2, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if raw1 == raw2 {
		t.Fatal("New() produced the same raw token twice")
	}
	if hash1 == hash2 {
		t.Fatal("New() produced the same hash twice")
	}
	if Hash(raw1) != hash1 {
		t.Fatalf("Hash(raw1) = %q, want %q (the hash New() returned for it)", Hash(raw1), hash1)
	}
	if Hash(raw2) != hash2 {
		t.Fatalf("Hash(raw2) = %q, want %q", Hash(raw2), hash2)
	}
}

func TestHashDoesNotLeakRawToken(t *testing.T) {
	raw, hash, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if hash == raw {
		t.Fatal("hash equals the raw token - storing it would be equivalent to storing the token itself")
	}
}
