package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Dispatcher interface {
	Dispatch(context.Context, Job) (Result, error)
}

type HTTPDispatcher struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

func (d HTTPDispatcher) Dispatch(ctx context.Context, job Job) (Result, error) {
	if err := job.Validate(); err != nil {
		return Result{}, err
	}
	base := strings.TrimRight(strings.TrimSpace(d.BaseURL), "/")
	if base == "" || strings.TrimSpace(d.Token) == "" {
		return Result{}, fmt.Errorf("runner dispatcher is not configured")
	}
	payload, err := json.Marshal(job)
	if err != nil {
		return Result{}, err
	}
	client := d.Client
	if client == nil {
		client = &http.Client{Timeout: time.Duration(job.TimeoutSeconds+30) * time.Second}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/jobs", bytes.NewReader(payload))
	if err != nil {
		return Result{}, err
	}
	request.Header.Set("Authorization", "Bearer "+d.Token)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return Result{}, fmt.Errorf("dispatch runner job: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxOutputBytes+64*1024))
	if err != nil {
		return Result{}, fmt.Errorf("read runner response: %w", err)
	}
	var result Result
	if err := json.Unmarshal(body, &result); err != nil {
		return Result{}, fmt.Errorf("decode runner response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		if result.Error == "" {
			result.Error = fmt.Sprintf("runner returned HTTP %d", response.StatusCode)
		}
		return result, fmt.Errorf("runner job failed: %s", result.Error)
	}
	return result, nil
}
