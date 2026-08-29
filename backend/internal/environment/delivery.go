package environment

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

var deliveryCheckPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:/-]{0,127}$`)

func (p DeliveryPolicy) Validate() error {
	p = p.Normalize()
	if p.ReleaseMode != ReleaseModeStandard && p.ReleaseMode != ReleaseModeGated {
		return fmt.Errorf("release_mode must be standard or release_gated")
	}
	if len(p.RequiredChecks) > 50 {
		return fmt.Errorf("at most 50 required CI checks are allowed")
	}
	seen := make(map[string]bool, len(p.RequiredChecks))
	for _, check := range p.RequiredChecks {
		check = strings.TrimSpace(check)
		if !deliveryCheckPattern.MatchString(check) || seen[strings.ToLower(check)] {
			return fmt.Errorf("required CI check names are invalid or duplicated")
		}
		seen[strings.ToLower(check)] = true
	}
	if err := validateReviewPolicy(p.Review); err != nil {
		return err
	}
	for _, level := range []RiskLevel{RiskLow, RiskNormal, RiskHigh, RiskCritical} {
		if err := validateRiskRule(level, p.RiskRule(level)); err != nil {
			return err
		}
	}
	return nil
}

func validateReviewPolicy(policy ReviewPolicy) error {
	if policy.MinApprovals < 0 || policy.MinApprovals > 20 {
		return fmt.Errorf("review min_approvals must be between 0 and 20")
	}
	if len(policy.RequiredRoles) > 20 || len(policy.PathRules) > 50 {
		return fmt.Errorf("review policy contains too many roles or path rules")
	}
	for _, role := range policy.RequiredRoles {
		if !validRole(role) {
			return fmt.Errorf("review role is invalid")
		}
	}
	for _, rule := range policy.PathRules {
		if strings.TrimSpace(rule.Pattern) == "" || len(rule.Pattern) > 200 || rule.MinApprovals < 0 || rule.MinApprovals > 20 || !validPathPattern(rule.Pattern) {
			return fmt.Errorf("path review rule is invalid")
		}
		for _, role := range rule.RequiredRoles {
			if !validRole(role) {
				return fmt.Errorf("path review role is invalid")
			}
		}
	}
	return nil
}

func validPathPattern(pattern string) bool {
	trimmed := strings.Trim(pattern, "/")
	if trimmed == "" {
		return false
	}
	for _, part := range strings.Split(trimmed, "/") {
		if part == "" {
			return false
		}
		if part == "**" {
			continue
		}
		if _, err := path.Match(part, "probe"); err != nil {
			return false
		}
	}
	return true
}

func validateRiskRule(level RiskLevel, rule RiskRule) error {
	if rule.MinApprovals < 0 || rule.MinApprovals > 20 {
		return fmt.Errorf("%s min_approvals must be between 0 and 20", level)
	}
	for _, role := range rule.RequiredRoles {
		if !validRole(role) {
			return fmt.Errorf("%s required role is invalid", level)
		}
	}
	return nil
}

func validRole(role string) bool {
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "" || len(role) > 64 {
		return false
	}
	for _, r := range role {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return false
		}
	}
	return true
}
