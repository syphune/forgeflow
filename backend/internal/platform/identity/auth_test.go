package identity

import "testing"

func TestNewTokenIsOpaqueAndHashesDifferently(t *testing.T) {
	first, _, firstHash, err := newToken("ff_pat_")
	if err != nil {
		t.Fatal(err)
	}
	second, _, secondHash, err := newToken("ff_pat_")
	if err != nil {
		t.Fatal(err)
	}
	if first == second || firstHash == secondHash {
		t.Fatal("tokens must be unpredictable and unique")
	}
	if hashToken(first) != firstHash {
		t.Fatal("token hash mismatch")
	}
}
