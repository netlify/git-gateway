package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitLabPathTraversalSanitization(t *testing.T) {
	makeTraversalTests(t, "/gitlab", "repository/files", "/projects/owner%2Frepo")
}

func TestGitLabPathTraversalWithEncodedSlashes(t *testing.T) {
	g := newProxyTestGateway(t)
	token := g.userToken()

	tests := []struct {
		target    string
		status    int
		forwarded string
	}{
		{"/gitlab/repository/files/content%2Fposts%2F..%2Fa.md", http.StatusOK, "/projects/owner%2Frepo/repository/files/content%2Fposts%2F..%2Fa.md"},
		{"/gitlab/repository/files/a%2F..%2F..%2F..%2Fvariables", http.StatusUnauthorized, ""},
		{"/gitlab/repository/files%2F..%2F..%2Fvariables", http.StatusUnauthorized, ""},
		// Leaves the project only when "a%2Fb%2Fc" is one segment, which is
		// how GitLab reads it.
		{"/gitlab/repository/files/a%2Fb%2Fc/../../../../files%2Fx/repository/files/f/raw", http.StatusBadRequest, ""},
	}

	for _, tt := range tests {
		code := g.serve(http.MethodGet, tt.target, token)
		assert.Equal(t, tt.status, code, tt.target)
		if tt.forwarded == "" {
			assert.Empty(t, g.forwarded, tt.target)
			continue
		}
		assert.Equal(t, []forwardedRequest{{http.MethodGet, tt.forwarded, "Bearer " + testOwnerToken}}, g.forwarded, tt.target)
	}
}

func TestGitLabGatewayForwardsAllowedRequests(t *testing.T) {
	g := newProxyTestGateway(t)
	token := g.userToken()

	tests := []struct {
		method string
		target string
		want   string
	}{
		{http.MethodGet, "/gitlab/repository/files/content%2Fposts%2Fa.md/raw?ref=main", "/projects/owner%2Frepo/repository/files/content%2Fposts%2Fa.md/raw?ref=main"},
		{http.MethodPut, "/gitlab/repository/files/content%2Fposts%2Fa.md", "/projects/owner%2Frepo/repository/files/content%2Fposts%2Fa.md"},
		{http.MethodGet, "/gitlab/repository/tree?path=content&ref=main", "/projects/owner%2Frepo/repository/tree?path=content&ref=main"},
		{http.MethodPost, "/gitlab/repository/commits", "/projects/owner%2Frepo/repository/commits"},
		{http.MethodGet, "/gitlab/repository/commits/abc123/statuses", "/projects/owner%2Frepo/repository/commits/abc123/statuses"},
		{http.MethodGet, "/gitlab/repository/compare?from=main&to=cms%2Fposts%2Fa", "/projects/owner%2Frepo/repository/compare?from=main&to=cms%2Fposts%2Fa"},
		{http.MethodGet, "/gitlab/repository/branches/cms%2Fposts%2Fa", "/projects/owner%2Frepo/repository/branches/cms%2Fposts%2Fa"},
		{http.MethodGet, "/gitlab/merge_requests?state=opened", "/projects/owner%2Frepo/merge_requests?state=opened"},
		{http.MethodPut, "/gitlab/merge_requests/3/merge", "/projects/owner%2Frepo/merge_requests/3/merge"},
	}

	for _, tt := range tests {
		code := g.serve(tt.method, tt.target, token)
		require.Equal(t, http.StatusOK, code, tt.target)
		assert.Equal(t, []forwardedRequest{{tt.method, tt.want, "Bearer " + testOwnerToken}}, g.forwarded, tt.target)
	}
}

func TestGitLabGatewayRejectsPathsOffTheAllowList(t *testing.T) {
	g := newProxyTestGateway(t)
	token := g.userToken()

	targets := []string{
		"/gitlab/variables",
		"/gitlab",
		"/gitlab/repository",
		"/gitlab/repository/archive",
		"/gitlab/repository/filesfoo",
		"/gitlab/merge_requestsfoo",
	}

	for _, target := range targets {
		code := g.serve(http.MethodGet, target, token)
		assert.Equal(t, http.StatusUnauthorized, code, target)
		assert.Empty(t, g.forwarded, target)
	}
}

func TestGitLabGatewayAppliesRoleRestrictions(t *testing.T) {
	g := newProxyTestGateway(t, "admin")

	code := g.serve(http.MethodGet, "/gitlab/repository/tree", g.userToken("editor"))
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Empty(t, g.forwarded)

	code = g.serve(http.MethodGet, "/gitlab/repository/tree", g.userToken("admin"))
	assert.Equal(t, http.StatusOK, code)
	assert.Len(t, g.forwarded, 1)
}
