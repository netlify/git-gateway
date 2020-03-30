package api

import (
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
)

// GitHubGateway acts as a proxy to GitHub
type GitHubGateway struct {
	proxy *httputil.ReverseProxy
}

var pathRegexp = regexp.MustCompile("^/github/?")
var allowedRegexp = regexp.MustCompile("^/github/((git|contents|pulls|branches|merges|statuses|compare|commits)/?|(issues/(\\d+)/labels))")

func NewGitHubGateway() *GitHubGateway {
	return &GitHubGateway{
		proxy: &httputil.ReverseProxy{
			Director:     director,
			Transport:    &GitHubTransport{},
			ErrorHandler: proxyErrorHandler,
		},
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

	log := getLogEntry(r)
	log.Infof("Proxying to GitHub: %v", r.URL.String())
}

func (gh *GitHubGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	config := getConfig(ctx)
	if config == nil || config.GitHub.AccessToken == "" {
		handleError(notFoundError("No GitHub Settings Configured"), w, r)
		return
	}

	if err := gh.authenticate(w, r); err != nil {
		handleError(unauthorizedError(err.Error()), w, r)
		return
	}

	endpoint := config.GitHub.Endpoint
	apiURL := singleJoiningSlash(endpoint, "/repos/"+config.GitHub.Repo)
	target, err := url.Parse(apiURL)
	if err != nil {
		handleError(internalServerError("Unable to process GitHub endpoint"), w, r)
		return
	}
	ctx = withProxyTarget(ctx, target)
	ctx = withAccessToken(ctx, config.GitHub.AccessToken)
	gh.proxy.ServeHTTP(w, r.WithContext(ctx))
}

func (gh *GitHubGateway) authenticate(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	claims := getClaims(ctx)
	config := getConfig(ctx)

	if claims == nil {
		return errors.New("Access to endpoint not allowed: no claims found in Bearer token")
	}

	if !allowedRegexp.MatchString(r.URL.Path) {
		return errors.New("Access to endpoint not allowed: this part of GitHub's API has been restricted")
	}

	if len(config.Roles) == 0 {
		return nil
	}

	roles, ok := claims.AppMetaData["roles"]
	if ok {
		roleStrings, _ := roles.([]interface{})
		for _, data := range roleStrings {
			role, _ := data.(string)
			for _, adminRole := range config.Roles {
				if role == adminRole {
					return nil
				}
			}
		}
	}

	return errors.New("Access to endpoint not allowed: your role doesn't allow access")
}

type GitHubTransport struct{}

func (t *GitHubTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := http.DefaultTransport.RoundTrip(r)
	if err == nil {
		// remove CORS headers from GitHub and use our own
		resp.Header.Del("Access-Control-Allow-Origin")
	}
	return resp, err
}
