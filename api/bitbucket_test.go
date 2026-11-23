package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBitBucketPathTraversalSanitization(t *testing.T) {
	makeTraversalTests(t, "/bitbucket", "src", "/repositories/owner/repo")
}

func TestBitBucketGatewayForwardsAllowedRequests(t *testing.T) {
	g := newProxyTestGateway(t)
	token := g.userToken()

	tests := []struct {
		method string
		target string
		want   string
	}{
		{http.MethodGet, "/bitbucket/src", "/repositories/owner/repo/src"},
		{http.MethodPost, "/bitbucket/src", "/repositories/owner/repo/src"},
		{http.MethodGet, "/bitbucket/src/master/content/posts/a.md", "/repositories/owner/repo/src/master/content/posts/a.md"},
		{http.MethodGet, "/bitbucket/src/master/content/?max_depth=2", "/repositories/owner/repo/src/master/content/?max_depth=2"},
	}

	for _, tt := range tests {
		code := g.serve(tt.method, tt.target, token)
		require.Equal(t, http.StatusOK, code, tt.target)
		assert.Equal(t, []forwardedRequest{{tt.method, tt.want, "Bearer " + testOwnerToken}}, g.forwarded, tt.target)
	}
}

func TestBitBucketGatewayRejectsPathsOffTheAllowList(t *testing.T) {
	g := newProxyTestGateway(t)
	token := g.userToken()

	targets := []string{
		"/bitbucket/pullrequests",
		"/bitbucket/pipelines_config/variables",
		"/bitbucket",
		"/bitbucket/srcfoo",
	}

	for _, target := range targets {
		code := g.serve(http.MethodGet, target, token)
		assert.Equal(t, http.StatusUnauthorized, code, target)
		assert.Empty(t, g.forwarded, target)
	}
}

func TestBitBucketGatewayAppliesRoleRestrictions(t *testing.T) {
	g := newProxyTestGateway(t, "admin")

	code := g.serve(http.MethodGet, "/bitbucket/src", g.userToken("editor"))
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Empty(t, g.forwarded)

	code = g.serve(http.MethodGet, "/bitbucket/src", g.userToken("admin"))
	assert.Equal(t, http.StatusOK, code)
	assert.Len(t, g.forwarded, 1)
}
