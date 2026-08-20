package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestValidateBridgeEndpoint(t *testing.T) {
	for _, test := range []struct {
		name string
		url  string
		want bool
	}{
		{name: "http", url: "http://localhost:18080/api/v1/mcp", want: true},
		{name: "https", url: "https://forgeflow.example.com/api/v1/mcp", want: true},
		{name: "missing host", url: "http:///api/v1/mcp", want: false},
		{name: "unsupported scheme", url: "file:///tmp/mcp", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateBridgeEndpoint(test.url)
			if (err == nil) != test.want {
				t.Fatalf("validateBridgeEndpoint(%q) error = %v, want valid = %v", test.url, err, test.want)
			}
		})
	}
}

func TestBearerTransportScopesUpstreamRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer token-1" {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		if request.Header.Get("X-Project-ID") != "project-1" {
			t.Errorf("project = %q", request.Header.Get("X-Project-ID"))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := &http.Client{Transport: bearerTransport{token: "token-1", projectID: "project-1"}}
	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
}

func TestBridgeRegistrationProxiesToolsAndResources(t *testing.T) {
	ctx := context.Background()
	upstream := sdk.NewServer(&sdk.Implementation{Name: "upstream", Version: "test"}, nil)
	upstream.AddTool(&sdk.Tool{Name: "echo", Description: "echo", InputSchema: map[string]any{"type": "object"}}, func(_ context.Context, request *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		return &sdk.CallToolResult{StructuredContent: request.Params.Arguments}, nil
	})
	upstream.AddResource(&sdk.Resource{URI: "task://demo/1", Name: "task"}, func(_ context.Context, request *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
		return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: request.Params.URI, MIMEType: "text/plain", Text: "ok"}}}, nil
	})
	upstream.AddResourceTemplate(&sdk.ResourceTemplate{URITemplate: "repo://{id}/module/{path}", Name: "module"}, func(_ context.Context, request *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
		return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: request.Params.URI, MIMEType: "text/plain", Text: "module"}}}, nil
	})

	upstreamClientTransport, upstreamServerTransport := sdk.NewInMemoryTransports()
	if _, err := upstream.Connect(ctx, upstreamServerTransport, nil); err != nil {
		t.Fatal(err)
	}
	upstreamClient := sdk.NewClient(&sdk.Implementation{Name: "bridge-test", Version: "test"}, nil)
	upstreamSession, err := upstreamClient.Connect(ctx, upstreamClientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer upstreamSession.Close()

	proxy := sdk.NewServer(&sdk.Implementation{Name: "proxy", Version: "test"}, nil)
	if err := registerBridgeTools(ctx, proxy, upstreamSession); err != nil {
		t.Fatal(err)
	}
	if err := registerBridgeResources(ctx, proxy, upstreamSession); err != nil {
		t.Fatal(err)
	}

	proxyClientTransport, proxyServerTransport := sdk.NewInMemoryTransports()
	if _, err := proxy.Connect(ctx, proxyServerTransport, nil); err != nil {
		t.Fatal(err)
	}
	proxyClient := sdk.NewClient(&sdk.Implementation{Name: "proxy-client", Version: "test"}, nil)
	proxySession, err := proxyClient.Connect(ctx, proxyClientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer proxySession.Close()

	tools, err := proxySession.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 1 || tools.Tools[0].Name != "echo" {
		t.Fatalf("proxied tools = %#v, err = %v", tools, err)
	}
	result, err := proxySession.CallTool(ctx, &sdk.CallToolParams{Name: "echo", Arguments: map[string]any{"value": "hello"}})
	if err != nil || result.IsError {
		t.Fatalf("proxied tool call = %#v, err = %v", result, err)
	}
	resources, err := proxySession.ListResources(ctx, nil)
	if err != nil || len(resources.Resources) != 1 {
		t.Fatalf("proxied resources = %#v, err = %v", resources, err)
	}
	resource, err := proxySession.ReadResource(ctx, &sdk.ReadResourceParams{URI: "task://demo/1"})
	if err != nil || len(resource.Contents) != 1 || resource.Contents[0].Text != "ok" {
		t.Fatalf("proxied resource = %#v, err = %v", resource, err)
	}
	templates, err := proxySession.ListResourceTemplates(ctx, nil)
	if err != nil || len(templates.ResourceTemplates) != 1 {
		t.Fatalf("proxied templates = %#v, err = %v", templates, err)
	}
}
