package autonomous

import "testing"

func TestPolicyNormalizeUsesSafeDefaults(t *testing.T) {
	tests := []struct {
		name  string
		input Policy
		want  Policy
	}{
		{name: "zero value keeps explicit booleans", input: Policy{}, want: Policy{Enabled: false, Providers: DefaultPolicy().Providers, Runtime: "server", MaxAttempts: 3, TimeoutSeconds: 3600, AutoRetry: false, AutoCreatePR: false, TestScope: "unresolved_only", MCPPermissions: DefaultPolicy().MCPPermissions, ExecutionProfile: "default"}},
		{name: "bounded values are preserved", input: Policy{Enabled: false, Providers: []string{"claude"}, Runtime: "desktop", MaxAttempts: 2, TimeoutSeconds: 90, AutoRetry: false, AutoCreatePR: false, TestScope: "full_regression", ExecutionProfile: "safe"}, want: Policy{Enabled: false, Providers: []string{"claude"}, Runtime: "desktop", MaxAttempts: 2, TimeoutSeconds: 90, AutoRetry: false, AutoCreatePR: false, TestScope: "full_regression", MCPPermissions: DefaultPolicy().MCPPermissions, ExecutionProfile: "safe"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.input.Normalize()
			if got.Enabled != tt.want.Enabled || got.Runtime != tt.want.Runtime || got.MaxAttempts != tt.want.MaxAttempts || got.TimeoutSeconds != tt.want.TimeoutSeconds || got.TestScope != tt.want.TestScope || got.ExecutionProfile != tt.want.ExecutionProfile {
				t.Fatalf("normalized policy = %#v, want %#v", got, tt.want)
			}
			if len(got.Providers) != len(tt.want.Providers) || len(got.MCPPermissions) != len(tt.want.MCPPermissions) {
				t.Fatalf("normalized policy collections = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestPositionsAreSortedUniqueAndBounded(t *testing.T) {
	got := uniquePositions([]int{4, 1, 4, 0, 10001, 2})
	want := []int{1, 2, 4}
	if len(got) != len(want) {
		t.Fatalf("positions = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("positions = %#v, want %#v", got, want)
		}
	}
}

func TestIntersectionNeverReintroducesPassedPositions(t *testing.T) {
	got := intersection([]int{3, 1, 2}, []int{1, 3})
	want := []int{1, 3}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("intersection = %#v, want %#v", got, want)
	}
}
