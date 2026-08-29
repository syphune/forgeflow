package environment

import "testing"

func TestDeliveryPolicyValidatesRecursivePathRules(t *testing.T) {
	policy := DefaultDeliveryPolicy()
	policy.Review.PathRules = []PathReviewRule{{
		Pattern:       "payments/**",
		MinApprovals:  2,
		RequiredRoles: []string{"security_reviewer"},
	}}
	if err := policy.Validate(); err != nil {
		t.Fatalf("recursive path rule should validate: %v", err)
	}

	policy.Review.PathRules[0].Pattern = "payments/["
	if err := policy.Validate(); err == nil {
		t.Fatal("malformed path rule should be rejected")
	}
}
