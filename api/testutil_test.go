package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/netlify/git-gateway/conf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

const (
	testJWTSecret  = "jwt-secret"
	testOwnerToken = "owner-token"
)

type forwardedRequest struct {
	Method        string
	RequestURI    string
	Authorization string
}

// proxyTestGateway serves requests through the full router, so that routing
// and JWT handling see the same raw URL they would in production, and records
// what reaches the upstream.
type proxyTestGateway struct {
	t         *testing.T
	roles     []string
	forwarded []forwardedRequest
	upstream  *httptest.Server
}

func newProxyTestGateway(t *testing.T, roles ...string) *proxyTestGateway {
	g := &proxyTestGateway{t: t, roles: roles}
	g.upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.forwarded = append(g.forwarded, forwardedRequest{
			Method: r.Method,
			// The GitLab director sends an absolute-form request target.
			RequestURI:    strings.TrimPrefix(r.RequestURI, "http://"+r.Host),
			Authorization: r.Header.Get("Authorization"),
		})
	}))
	t.Cleanup(g.upstream.Close)

	// Without a valid cached token, an accepted Bitbucket request fetches one
	// from bitbucket.org.
	previous := currentTokenSource
	currentTokenSource = &notifyRefreshTokenSource{t: &oauth2.Token{AccessToken: testOwnerToken}}
	t.Cleanup(func() { currentTokenSource = previous })

	return g
}

func (g *proxyTestGateway) userToken(roles ...interface{}) string {
	claims := &GatewayClaims{AppMetaData: map[string]interface{}{"roles": roles}}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testJWTSecret))
	require.NoError(g.t, err)
	return token
}

func (g *proxyTestGateway) serve(method, target, bearer string) int {
	g.forwarded = nil

	ctx, err := WithInstanceConfig(context.Background(), &conf.Configuration{
		JWT: conf.JWTConfiguration{Secret: testJWTSecret},
		GitHub: conf.GitHubConfig{
			AccessToken: testOwnerToken,
			Endpoint:    g.upstream.URL,
			Repo:        "owner/repo",
		},
		GitLab: conf.GitLabConfig{
			AccessToken: testOwnerToken,
			Endpoint:    g.upstream.URL,
			Repo:        "owner/repo",
		},
		BitBucket: conf.BitBucketConfig{
			RefreshToken: "refresh-token",
			Endpoint:     g.upstream.URL,
			Repo:         "owner/repo",
		},
		Roles: g.roles,
	}, "")
	require.NoError(g.t, err)
	api := NewAPIWithVersion(ctx, &conf.GlobalConfiguration{}, nil, "test")

	r := httptest.NewRequest(method, target, nil)
	r.Header.Set("Authorization", "Bearer "+bearer)
	w := httptest.NewRecorder()
	api.handler.ServeHTTP(w, r)
	return w.Code
}

// makeTraversalTests runs the path traversal cases that every gateway has to
// handle the same way. route is where the gateway is mounted, allowedPath is
// an allow-listed path below it that takes arbitrary sub-paths, and
// upstreamRepo is the path the provider serves the repository's API from.
func makeTraversalTests(t *testing.T, route, allowedPath, upstreamRepo string) {
	g := newProxyTestGateway(t)
	token := g.userToken()

	base := route + "/" + allowedPath
	upstream := upstreamRepo + "/" + allowedPath

	// This many dot segments climb from allowedPath to the repository root.
	// One more leaves the repository.
	toRoot := strings.Count(allowedPath, "/") + 1
	out := toRoot + 1
	climb := func(segment string, n int) string {
		return base + "/" + strings.Repeat(segment+"/", n)
	}

	tests := []struct {
		name      string
		target    string
		status    int
		forwarded string
	}{
		{"dot segments inside the allowed path", base + "/a/../b", http.StatusOK, upstream + "/a/../b"},
		{"dots that are not dot segments", base + "/a...b/..c/d..", http.StatusOK, upstream + "/a...b/..c/d.."},
		{"literal percent sign", base + "/50%25off.png", http.StatusOK, upstream + "/50%25off.png"},
		{"trailing literal percent sign", base + "/100%25", http.StatusOK, upstream + "/100%25"},

		{"dot segments to the repository root", climb("..", toRoot) + "x", http.StatusUnauthorized, ""},
		{"dot segments ending at the repository root", strings.TrimSuffix(climb("..", toRoot), "/"), http.StatusUnauthorized, ""},
		{"encoded dot segments to the repository root", climb("%2e%2e", toRoot) + "x", http.StatusUnauthorized, ""},
		{"doubly encoded dot segments to the repository root", climb("%252e%252e", toRoot) + "x", http.StatusUnauthorized, ""},

		{"dot segments", climb("..", out) + "other/f", http.StatusBadRequest, ""},
		{"encoded dot segments", climb("%2e%2e", out) + "other/f", http.StatusBadRequest, ""},
		{"uppercase encoded dot segments", climb("%2E%2E", out) + "other/f", http.StatusBadRequest, ""},
		{"partly encoded dot segments", climb(".%2e", out) + "other/f", http.StatusBadRequest, ""},
		{"doubly encoded dot segments", climb("%252e%252e", out) + "other/f", http.StatusBadRequest, ""},
		{"encoded slashes", base + "/" + strings.Repeat("..%2f", out) + "other/f", http.StatusBadRequest, ""},
		{"doubly encoded slashes", base + "/" + strings.Repeat("..%252f", out) + "other/f", http.StatusBadRequest, ""},
		{"semicolon path parameters", climb("..;", out) + "other/f", http.StatusBadRequest, ""},
		{"encoded semicolon path parameters", climb("..%3b", out) + "other/f", http.StatusBadRequest, ""},
		{"uppercase encoded semicolon path parameters", climb("..%3B", out) + "other/f", http.StatusBadRequest, ""},
		{"doubly encoded semicolon path parameters", climb("..%253b", out) + "other/f", http.StatusBadRequest, ""},
		{"encoded backslashes", climb("..%5c", out) + "other/f", http.StatusBadRequest, ""},
		{"encoded backslash separators", base + "/" + strings.Repeat("..%5c", out) + "other/f", http.StatusBadRequest, ""},
		{"encoded dot segments beside a malformed escape", base + "/%25zz/" + strings.Repeat("%252e%252e/", out+1) + "other/f", http.StatusBadRequest, ""},
		{"semicolon in a file name", base + "/a;b", http.StatusBadRequest, ""},
		{"encoded semicolon in a file name", base + "/a%3Bb", http.StatusBadRequest, ""},
		{"path encoded too deeply", base + "/%25252525252e", http.StatusBadRequest, ""},
		{"semicolon in the query", base + "/f?ref=a;b", http.StatusBadRequest, ""},
		{"encoded semicolon in the query", base + "/f?ref=a%3Bb", http.StatusBadRequest, ""},
		{"doubly encoded semicolon in the query", base + "/f?ref=a%253Bb", http.StatusBadRequest, ""},
		{"encoded semicolon beside a malformed escape in the query", base + "/f?ref=%zz%3B", http.StatusBadRequest, ""},
		{"query encoded too deeply", base + "/f?ref=%25252525253B", http.StatusBadRequest, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, method := range []string{http.MethodGet, http.MethodPut} {
				status := g.serve(method, tt.target, token)
				assert.Equal(t, tt.status, status, "%s %s", method, tt.target)
				if tt.forwarded == "" {
					assert.Empty(t, g.forwarded, "%s %s", method, tt.target)
					continue
				}
				assert.Equal(t, []forwardedRequest{{method, tt.forwarded, "Bearer " + testOwnerToken}}, g.forwarded, "%s %s", method, tt.target)
			}
		})
	}
}
