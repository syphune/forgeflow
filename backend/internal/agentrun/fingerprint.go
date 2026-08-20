package agentrun

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type fingerprintPayload struct {
	Version         int             `json:"version"`
	RepositoryID    string          `json:"repository_id"`
	BaseSHA         string          `json:"base_sha"`
	Branch          string          `json:"branch"`
	AgentProvider   string          `json:"agent_provider"`
	AgentName       string          `json:"agent_name"`
	Model           string          `json:"model"`
	ExecutionInputs ExecutionInputs `json:"execution_inputs"`
	ExecutionPolicy map[string]any  `json:"execution_policy"`
}

func approvalFingerprint(input CreateInput) (string, error) {
	payload := fingerprintPayload{
		Version:         ApprovalFingerprintVersion,
		RepositoryID:    strings.TrimSpace(input.RepositoryID),
		BaseSHA:         strings.TrimSpace(input.BaseSHA),
		Branch:          strings.TrimSpace(input.Branch),
		AgentProvider:   strings.TrimSpace(input.AgentProvider),
		AgentName:       strings.TrimSpace(input.AgentName),
		Model:           strings.TrimSpace(input.Model),
		ExecutionInputs: normalizeExecutionInputs(input.ExecutionInputs),
		ExecutionPolicy: normalizeMap(input.ExecutionPolicy),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func fingerprintForRun(run Run) (string, error) {
	return approvalFingerprint(CreateInput{
		RepositoryID:    run.RepositoryID,
		BaseSHA:         run.BaseSHA,
		Branch:          run.Branch,
		AgentProvider:   run.AgentProvider,
		AgentName:       run.AgentName,
		Model:           run.Model,
		ExecutionInputs: run.ExecutionInputs,
		ExecutionPolicy: run.ExecutionPolicy,
	})
}

func encodeExecutionPolicy(inputs ExecutionInputs, policy map[string]any) ([]byte, error) {
	return json.Marshal(map[string]any{
		"execution_inputs": normalizeExecutionInputs(inputs),
		"execution_policy": normalizeMap(policy),
	})
}

func decodeExecutionPolicy(raw []byte, run *Run) error {
	if len(raw) == 0 {
		return nil
	}
	var document struct {
		ExecutionInputs ExecutionInputs `json:"execution_inputs"`
		ExecutionPolicy map[string]any  `json:"execution_policy"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return fmt.Errorf("decode AgentRun execution policy: %w", err)
	}
	run.ExecutionInputs = normalizeExecutionInputs(document.ExecutionInputs)
	run.ExecutionPolicy = normalizeMap(document.ExecutionPolicy)
	return nil
}

func normalizeExecutionInputs(input ExecutionInputs) ExecutionInputs {
	input.Prompt = strings.TrimSpace(input.Prompt)
	input.WorktreeDiffHash = strings.TrimSpace(input.WorktreeDiffHash)
	input.ExecutionProfile = strings.TrimSpace(input.ExecutionProfile)
	input.ToolPermissions = normalizeStrings(input.ToolPermissions)
	input.MCPPermissions = normalizeStrings(input.MCPPermissions)
	input.TestCasePositions = normalizeInts(input.TestCasePositions)
	input.AgentConfiguration = normalizeMap(input.AgentConfiguration)
	input.SandboxPolicy = normalizeMap(input.SandboxPolicy)
	input.NetworkPolicy = normalizeMap(input.NetworkPolicy)
	return input
}

func normalizeInts(values []int) []int {
	result := make([]int, 0, len(values))
	seen := make(map[int]struct{}, len(values))
	for _, value := range values {
		if value < 1 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Ints(result)
	return result
}

func normalizeStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func normalizeMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return map[string]any{}
	}
	result := make(map[string]any, len(values))
	for key, value := range values {
		result[strings.TrimSpace(key)] = value
	}
	return result
}
