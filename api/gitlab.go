package api

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
)

// GitLabGateway acts as a proxy to Gitlab
type GitLabGateway struct {
	proxy *httputil.ReverseProxy
}

var gitlabPathRegexp = regexp.MustCompile("^/gitlab/?")
var gitlabAllowedRegexp = regexp.MustCompile("^/gitlab/repository/(files|commits|tree)/?")

func NewGitLabGateway() *GitLabGateway {
	return &GitLabGateway{
		proxy: &httputil.ReverseProxy{
			Director:  gitlabDirector,
			Transport: &GitLabTransport{},
		},
	}
}

func gitlabDirector(r *http.Request) {
	ctx := r.Context()
	target := getProxyTarget(ctx)
	accessToken := getAccessToken(ctx)

	targetQuery := target.RawQuery
	r.Host = target.Host
	r.URL.Scheme = target.Scheme
	r.URL.Host = target.Host
	// We need to set URL.Opaque using the target and r.URL EscapedPath
	// methods, because the Go stdlib URL parsing automatically converts
	// %2F to / in URL paths, and GitLab requires %2F to be preserved
	// as-is.
	r.URL.Opaque = "//" + target.Host + singleJoiningSlash(target.EscapedPath(), gitlabPathRegexp.ReplaceAllString(r.URL.EscapedPath(), "/"))
	if targetQuery == "" || r.URL.RawQuery == "" {
		r.URL.RawQuery = targetQuery + r.URL.RawQuery
	} else {
		r.URL.RawQuery = targetQuery + "&" + r.URL.RawQuery
	}
	if _, ok := r.Header["User-Agent"]; !ok {
		// explicitly disable User-Agent so it's not set to default value
		r.Header.Set("User-Agent", "")
	}

	config := getConfig(ctx)
	tokenType := config.GitLab.AccessTokenType

	if tokenType == "personal_access" {
		// Private access token
		r.Header.Del("Authorization")
		if r.Method != http.MethodOptions {
			r.Header.Set("Private-Token", accessToken)
		}
	} else {
		// OAuth token
		r.Header.Del("Authorization")
		if r.Method != http.MethodOptions {
			r.Header.Set("Authorization", "Bearer "+accessToken)
		}
	}

	log := getLogEntry(r)
	log.Infof("Proxying to GitLab: %v", r.URL.String())
}

func (gl *GitLabGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	config := getConfig(ctx)
	if config == nil || config.GitLab.AccessToken == "" {
		handleError(notFoundError("No GitLab Settings Configured"), w, r)
		return
	}

	if !gitlabAllowedRegexp.MatchString(r.URL.Path) {
		handleError(unauthorizedError("Access to endpoint not allowed: this part of GitLab's API has been restricted"), w, r)
		return
	}

	endpoint := config.GitLab.Endpoint
	// repos in the form of userName/repoName must be encoded as
	// userName%2FrepoName
	repo := url.PathEscape(config.GitLab.Repo)
	apiURL := singleJoiningSlash(endpoint, "/projects/"+repo)
	target, err := url.Parse(apiURL)
	if err != nil {
		handleError(internalServerError("Unable to process GitLab endpoint"), w, r)
		return
	}
	ctx = withProxyTarget(ctx, target)
	ctx = withAccessToken(ctx, config.GitLab.AccessToken)
	gl.proxy.ServeHTTP(w, r.WithContext(ctx))
}

var gitlabLinkRegex = regexp.MustCompile("<(.*?)>")
var gitlabLinkRelRegex = regexp.MustCompile("rel=\"(.*?)\"")

func rewriteGitlabLinkEntry(linkEntry, endpointAPIURL, proxyAPIURL string) string {
	linkAndRel := strings.Split(strings.TrimSpace(linkEntry), ";")
	if len(linkAndRel) != 2 {
		return linkEntry
	}

	linkMatch := gitlabLinkRegex.FindStringSubmatch(linkAndRel[0])
	if len(linkMatch) < 2 {
		return linkEntry
	}

	relMatch := gitlabLinkRelRegex.FindStringSubmatch(linkAndRel[1])
	if len(relMatch) < 2 {
		return linkEntry
	}

	proxiedLink := proxyAPIURL + strings.TrimPrefix(linkMatch[1], endpointAPIURL)
	rel := relMatch[1]
	return "<" + proxiedLink + ">; rel=\"" + rel + "\""
}

func rewriteGitlabLinks(linkHeader, endpointAPIURL, proxyAPIURL string) string {
	linkEntries := strings.Split(linkHeader, ",")
	finalLinkEntries := make([]string, len(linkEntries), len(linkEntries))
	for i, linkEntry := range linkEntries {
		finalLinkEntries[i] = rewriteGitlabLinkEntry(linkEntry, endpointAPIURL, proxyAPIURL)
	}
	finalLinkHeader := strings.Join(finalLinkEntries, ",")
	return finalLinkHeader
}

type GitLabTransport struct{}

func (t *GitLabTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	ctx := r.Context()
	config := getConfig(ctx)
	resp, err := http.DefaultTransport.RoundTrip(r)
	if err == nil {
		// remove CORS headers from GitLab and use our own
		resp.Header.Del("Access-Control-Allow-Origin")
		linkHeader := resp.Header.Get("Link")
		if linkHeader != "" {
			endpoint := config.GitLab.Endpoint
			repo := url.PathEscape(config.GitLab.Repo)
			apiURL := singleJoiningSlash(endpoint, "/projects/"+repo)
			newLinkHeader := rewriteGitlabLinks(linkHeader, apiURL, "")
			resp.Header.Set("Link", newLinkHeader)
		}
	}

	return resp, err
}
