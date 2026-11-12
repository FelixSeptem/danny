package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
)

// The OpenAI Responses request shape allows `stream` to be omitted, and omitting
// it means false. Some OpenAI-compatible gateways instead treat an omitted flag
// as a streaming call, then try to JSON-decode the SSE body and return
// 500 bad_response_body ("invalid character 'd'"). That is a provider defect, so
// the harness absorbs it only when an operator asks for it, and records in the
// evidence bundle that it did.
const (
	liveProviderShimEnv = "DANNY_LIVE_PROVIDER_SHIM"
	shimStreamDefault   = "stream-default"
	shimCompatRecord    = "stream-field-injected"
)

// liveGateway returns the endpoint Danny should dial plus the compat record to
// publish. Without an explicit opt-in it returns the operator's endpoint and no
// translation, which keeps the normal path byte-for-byte unmodified.
func liveGateway(endpoint string) (string, string, *providerShim, error) {
	mode := strings.TrimSpace(os.Getenv(liveProviderShimEnv))
	if mode == "" {
		return endpoint, "", nil, nil
	}
	if mode != shimStreamDefault {
		return "", "", nil, fmt.Errorf("%s=%q is not a supported compat mode (supported: %s)", liveProviderShimEnv, mode, shimStreamDefault)
	}
	shim, err := startProviderShim(endpoint)
	if err != nil {
		return "", "", nil, err
	}
	return shim.baseURL, shimCompatRecord, shim, nil
}

type providerShim struct {
	server  *http.Server
	baseURL string
	patched atomic.Int64
}

// startProviderShim forwards every request to the real upstream and adds the
// spec-default `"stream": false` when the client omitted the field. Requests
// that already carry a stream flag are forwarded untouched.
func startProviderShim(upstream string) (*providerShim, error) {
	target, err := url.Parse(upstream)
	if err != nil {
		return nil, fmt.Errorf("provider shim: parse upstream %q: %w", upstream, err)
	}
	if target.Scheme == "" || target.Host == "" {
		return nil, fmt.Errorf("provider shim: upstream %q needs scheme and host", upstream)
	}
	basePath := strings.TrimSuffix(target.Path, "/")
	shim := &providerShim{}
	// Rewrite (not the deprecated Director) also rewrites the outbound Host
	// to the target, which is what gateways behind a CDN require; a mismatched
	// Host is answered with 403 before the body is ever inspected.
	proxy := &httputil.ReverseProxy{Rewrite: func(pr *httputil.ProxyRequest) {
		pr.SetURL(&url.URL{Scheme: target.Scheme, Host: target.Host})
		if pr.Out.Body == nil || pr.Out.Body == http.NoBody {
			return
		}
		raw, readErr := io.ReadAll(pr.Out.Body)
		_ = pr.Out.Body.Close()
		if readErr != nil {
			pr.Out.Body = http.NoBody
			pr.Out.ContentLength = 0
			pr.Out.Header.Del("Content-Length")
			return
		}
		body, patched := injectExplicitStream(raw)
		if patched {
			shim.patched.Add(1)
		}
		pr.Out.Body = io.NopCloser(bytes.NewReader(body))
		pr.Out.ContentLength = int64(len(body))
		pr.Out.Header.Set("Content-Length", strconv.Itoa(len(body)))
	}}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("provider shim: listen: %w", err)
	}
	shim.baseURL = "http://" + listener.Addr().String() + basePath
	shim.server = &http.Server{Handler: proxy}
	go func() { _ = shim.server.Serve(listener) }()
	return shim, nil
}

func (s *providerShim) Close() error {
	if s == nil || s.server == nil {
		return nil
	}
	return s.server.Close()
}

func (s *providerShim) PatchedRequests() int64 {
	if s == nil {
		return 0
	}
	return s.patched.Load()
}

// injectExplicitStream reports whether it changed the body, so the caller can
// count only the requests that actually needed the workaround.
func injectExplicitStream(raw []byte) ([]byte, bool) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return raw, false
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return raw, false
	}
	if _, present := doc["stream"]; present {
		return raw, false
	}
	doc["stream"] = json.RawMessage("false")
	encoded, err := json.Marshal(doc)
	if err != nil {
		return raw, false
	}
	return encoded, true
}
