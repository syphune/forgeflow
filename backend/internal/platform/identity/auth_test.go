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

func TestActorForProjectDoesNotUnionProjectCapabilities(t *testing.T) {
	actor := Actor{
		Capabilities: map[string]bool{CapabilityProjectRead: true},
		ProjectCapabilities: map[string]map[string]bool{
			"project-a": {CapabilityWorkItemEdit: true},
			"project-b": {CapabilityRepositoryRead: true},
		},
	}

	if scoped := actor.ForProject("project-a"); !scoped.Has(CapabilityWorkItemEdit) || scoped.Has(CapabilityRepositoryRead) {
		t.Fatalf("project-a capabilities = %#v", scoped.Capabilities)
	}
	if scoped := actor.ForProject("project-b"); !scoped.Has(CapabilityRepositoryRead) || scoped.Has(CapabilityWorkItemEdit) {
		t.Fatalf("project-b capabilities = %#v", scoped.Capabilities)
	}
}
