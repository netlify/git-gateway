package api

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
)

// GitHubGateway acts as a proxy to GitHub
type GitHubGateway struct {
	proxy *httputil.ReverseProxy
}

const defaultEndpoint = "https://api.github.com"

var pathRegexp = regexp.MustCompile("^/github/?")

func NewGitHubGateway() *GitHubGateway {
	return &GitHubGateway{
		proxy: &httputil.ReverseProxy{Director: director},
	}
}

func director(r *http.Request) {
	ctx := r.Context()
	target := getProxyTarget(ctx)
	accessToken := getAccessToken(ctx)

	targetQuery := target.RawQuery
	r.Host = target.Host
	r.URL.Scheme = target.Scheme
	r.URL.Host = target.Host
	r.URL.Path = singleJoiningSlash(target.Path, pathRegexp.ReplaceAllString(r.URL.Path, "/"))
	if targetQuery == "" || r.URL.RawQuery == "" {
		r.URL.RawQuery = targetQuery + r.URL.RawQuery
	} else {
		r.URL.RawQuery = targetQuery + "&" + r.URL.RawQuery
	}
	if _, ok := r.Header["User-Agent"]; !ok {
		// explicitly disable User-Agent so it's not set to default value
		r.Header.Set("User-Agent", "")
	}
	if r.Method != http.MethodOptions {
		r.Header.Set("Authorization", "Bearer "+accessToken)
	}
}

func (gh *GitHubGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	config := getConfig(ctx)
	if config == nil || config.GitHub.AccessToken == "" {
		handleError(notFoundError("No GitHub Settings Configured"), w, r)
		return
	}

	if !gh.authenticate(w, r) {
		handleError(unauthorizedError("Not authorized to access this endpoint"), w, r)
		return
	}

	var endpoint string
	if config.GitHub.Endpoint != "" {
		endpoint = config.GitHub.Endpoint
	} else {
		endpoint = defaultEndpoint
	}

	target, err := url.Parse(endpoint)
	if err != nil {
		handleError(internalServerError("Unable to process GitHub endpoint"), w, r)
		return
	}
	ctx = withProxyTarget(ctx, target)
	ctx = withAccessToken(ctx, config.GitHub.AccessToken)
	gh.proxy.ServeHTTP(w, r.WithContext(ctx))
}

func (gh *GitHubGateway) authenticate(w http.ResponseWriter, r *http.Request) bool {
	ctx := r.Context()
	claims := getClaims(ctx)
	adminRoles := getRoles(ctx)
	config := getConfig(ctx)

	if claims == nil {
		return false
	}

	if config.GitHub.Repo != "" {
		repoRegexp := regexp.MustCompile("^/github/repos/" + config.GitHub.Repo + "/?")
		if !repoRegexp.MatchString(r.URL.Path) {
			return false
		}
	}

	roles, ok := claims.AppMetaData["roles"]
	if ok {
		roleStrings, _ := roles.([]interface{})
		for _, data := range roleStrings {
			role, _ := data.(string)
			for _, adminRole := range adminRoles {
				if role == adminRole.Name {
					return true
				}
			}
		}
	}

	return false
}
