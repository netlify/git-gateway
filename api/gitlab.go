package api

import (
	"bytes"
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"
)

// GitLabGateway acts as a proxy to Gitlab
type GitLabGateway struct {
	proxy *httputil.ReverseProxy
}

var gitlabPathRegexp = regexp.MustCompile("^/gitlab/?")
var gitlabAllowedRegexp = regexp.MustCompile("^/gitlab/merge_requests|(repository/(files|commits|tree|compare|branches))/?")

func NewGitLabGateway() *GitLabGateway {
	return &GitLabGateway{
		proxy: &httputil.ReverseProxy{
			Director:     gitlabDirector,
			Transport:    &GitLabTransport{},
			ErrorHandler: proxyErrorHandler,
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

	// remove header which causes false positives for blocking on Gitlab loadbalancers
	r.Header.Del("Client-IP")

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
	log.WithField("token_type", tokenType).
		Infof("Proxying to GitLab: %v", r.URL.String())
}

func (gl *GitLabGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	config := getConfig(ctx)
	if config == nil || config.GitLab.AccessToken == "" {
		handleError(notFoundError("No GitLab Settings Configured"), w, r)
		return
	}

	if err := gl.authenticate(w, r); err != nil {
		handleError(unauthorizedError(err.Error()), w, r)
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

func (gl *GitLabGateway) authenticate(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	claims := getClaims(ctx)
	config := getConfig(ctx)

	if claims == nil {
		return errors.New("Access to endpoint not allowed: no claims found in Bearer token")
	}

	if !gitlabAllowedRegexp.MatchString(r.URL.Path) {
		return errors.New("Access to endpoint not allowed: this part of GitLab's API has been restricted")
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

		logEntrySetFields(r, logrus.Fields{
			"gitlab_ratelimit_remaining": r.Header.Get("ratelimit-remaining"),
			"gitlab_request_id":          r.Header.Get("X-Request-Id"),
			"gitlab_lb":                  resp.Header.Get("gitlab-lb"),
		})

		if resp.StatusCode >= http.StatusInternalServerError {
			log := getLogEntry(r)

			bodyContent, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				log.WithError(err).Warn("Failed reading response body while handling server error")
				bodyContent = []byte{}
			}
			resp.Body = ioutil.NopCloser(bytes.NewBuffer(bodyContent))

			log.WithFields(logrus.Fields{
				"status": resp.StatusCode,
				"body":   string(bodyContent),
			}).Warn("Proxied host returned server error")
		}
	}

	return resp, err
}
