package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitHubPathTraversalSanitization(t *testing.T) {
	makeTraversalTests(t, "/github", "contents", "/repos/owner/repo")
}

func TestGitHubGatewayForwardsAllowedRequests(t *testing.T) {
	g := newProxyTestGateway(t)
	token := g.userToken()

	tests := []struct {
		method string
		target string
		want   string
	}{
		{http.MethodGet, "/github/contents/README", "/repos/owner/repo/contents/README"},
		{http.MethodPut, "/github/contents/posts/a.md", "/repos/owner/repo/contents/posts/a.md"},
		{http.MethodGet, "/github/contents/posts/a.md?ref=cms%2Fposts%2Fa", "/repos/owner/repo/contents/posts/a.md?ref=cms%2Fposts%2Fa"},
		{http.MethodGet, "/github/contents", "/repos/owner/repo/contents"},
		{http.MethodGet, "/github/contents/", "/repos/owner/repo/contents/"},
		{http.MethodGet, "/github/git/trees/main:content/posts", "/repos/owner/repo/git/trees/main:content/posts"},
		{http.MethodPost, "/github/git/blobs", "/repos/owner/repo/git/blobs"},
		{http.MethodGet, "/github/pulls?head=owner:cms/posts/a", "/repos/owner/repo/pulls?head=owner:cms/posts/a"},
		{http.MethodGet, "/github/branches/main", "/repos/owner/repo/branches/main"},
		{http.MethodPost, "/github/merges", "/repos/owner/repo/merges"},
		{http.MethodGet, "/github/statuses/abc123", "/repos/owner/repo/statuses/abc123"},
		{http.MethodGet, "/github/compare/main...cms/posts/a", "/repos/owner/repo/compare/main...cms/posts/a"},
		{http.MethodGet, "/github/commits/abc123/status", "/repos/owner/repo/commits/abc123/status"},
		{http.MethodPut, "/github/issues/12/labels", "/repos/owner/repo/issues/12/labels"},
		{http.MethodDelete, "/github/issues/12/labels/bug", "/repos/owner/repo/issues/12/labels/bug"},
	}

	for _, tt := range tests {
		code := g.serve(tt.method, tt.target, token)
		require.Equal(t, http.StatusOK, code, tt.target)
		assert.Equal(t, []forwardedRequest{{tt.method, tt.want, "Bearer " + testOwnerToken}}, g.forwarded, tt.target)
	}
}

func TestGitHubGatewayRejectsPathsOffTheAllowList(t *testing.T) {
	g := newProxyTestGateway(t)
	token := g.userToken()

	targets := []string{
		"/github/users/whatever",
		"/github/hooks",
		"/github",
		"/github/contentsfoo",
		"/github/git-upload-pack",
		"/github/issues/12/labelsfoo",
		"/github/issues/12/comments",
		"/github/issues/x/labels",
	}

	for _, target := range targets {
		code := g.serve(http.MethodGet, target, token)
		assert.Equal(t, http.StatusUnauthorized, code, target)
		assert.Empty(t, g.forwarded, target)
	}
}

func TestGitHubGatewayAuthentication(t *testing.T) {
	t.Run("rejects an invalid JWT", func(t *testing.T) {
		g := newProxyTestGateway(t)

		code := g.serve(http.MethodGet, "/github/contents/README", "not-a-jwt")
		assert.Equal(t, http.StatusUnauthorized, code)
		assert.Empty(t, g.forwarded)
	})

	t.Run("applies role restrictions", func(t *testing.T) {
		g := newProxyTestGateway(t, "admin")

		code := g.serve(http.MethodGet, "/github/contents/README", g.userToken("editor"))
		assert.Equal(t, http.StatusUnauthorized, code)
		assert.Empty(t, g.forwarded)

		code = g.serve(http.MethodGet, "/github/contents/README", g.userToken("admin"))
		assert.Equal(t, http.StatusOK, code)
		assert.Len(t, g.forwarded, 1)
	})
}
