package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/bitbucket"
)

type BitBucketGateway struct {
	proxy *httputil.ReverseProxy
}

func NewBitBucketGateway() *BitBucketGateway {
	return &BitBucketGateway{
		proxy: &httputil.ReverseProxy{
			Director:  bitbucketDirector,
			Transport: &BitBucketTransport{},
		},
	}
}

var bitbucketPathRegexp = regexp.MustCompile("^/bitbucket/?")
var bitbucketAllowedRegexp = regexp.MustCompile("^/bitbucket/src/?")

var bitbucketTokenExpirationMessageRegexp = regexp.MustCompile("(?i)^access token expired")
var currentAccessToken *oauth2.Token

type notifyRefreshTokenSource struct {
	new oauth2.TokenSource
	mu  sync.Mutex // guards t
	t   *oauth2.Token
}

func (s *notifyRefreshTokenSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.t.Valid() {
		return s.t, nil
	}
	t, err := s.new.Token()
	if err != nil {
		return nil, err
	}
	s.t = t
	return t, nil
}

var currentTokenSource *notifyRefreshTokenSource

func getTokenSource(ctx context.Context) *notifyRefreshTokenSource {
	config := getConfig(ctx)
	if currentTokenSource != nil {
		return currentTokenSource
	}

	blankToken := &oauth2.Token{
		AccessToken:  "",
		RefreshToken: config.BitBucket.RefreshToken,
	}
	oauthConfig := &oauth2.Config{
		ClientID:     config.BitBucket.ClientID,
		ClientSecret: config.BitBucket.ClientSecret,
		Endpoint:     bitbucket.Endpoint,
	}
	tokenSource := &notifyRefreshTokenSource{
		new: oauthConfig.TokenSource(ctx, blankToken),
		t:   blankToken,
	}
	currentTokenSource = tokenSource
	return tokenSource
}

func bitbucketDirector(r *http.Request) {
	ctx := r.Context()
	target := getProxyTarget(ctx)
	accessToken := getAccessToken(ctx)

	targetQuery := target.RawQuery
	r.Host = target.Host
	r.URL.Scheme = target.Scheme
	r.URL.Host = target.Host
	r.URL.Path = singleJoiningSlash(target.Path, bitbucketPathRegexp.ReplaceAllString(r.URL.Path, "/"))
	if targetQuery == "" || r.URL.RawQuery == "" {
		r.URL.RawQuery = targetQuery + r.URL.RawQuery
	} else {
		r.URL.RawQuery = targetQuery + "&" + r.URL.RawQuery
	}
	if _, ok := r.Header["User-Agent"]; !ok {
		r.Header.Set("User-Agent", "")
	}

	if r.Method != http.MethodOptions {
		r.Header.Set("Authorization", "Bearer "+accessToken)
	}

	log := getLogEntry(r)
	log.Infof("Proxying to BitBucket: %v", r.URL.String(), r.Header.Get("Authorization"))
}

func (bb *BitBucketGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	config := getConfig(ctx)
	if config == nil || config.BitBucket.RefreshToken == "" {
		handleError(notFoundError("No BitBucket Settings Configured"), w, r)
		return
	}

	if err := bb.authenticate(w, r); err != nil {
		handleError(unauthorizedError(err.Error()), w, r)
		return
	}

	endpoint := config.BitBucket.Endpoint
	apiURL := singleJoiningSlash(endpoint, "/repositories/"+config.BitBucket.Repo)
	target, err := url.Parse(apiURL)
	if err != nil {
		handleError(internalServerError("Unable to process BitBucket endpoint"), w, r)
		return
	}

	tokenSource := getTokenSource(ctx)
	token, err := tokenSource.Token()
	if err != nil {
		handleError(internalServerError("Unable to process BitBucket endpoint"), w, r)
	}

	ctx = withProxyTarget(ctx, target)
	ctx = withAccessToken(ctx, token.AccessToken)
	bb.proxy.ServeHTTP(w, r.WithContext(ctx))
}

func (bb *BitBucketGateway) authenticate(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	claims := getClaims(ctx)
	config := getConfig(ctx)

	if claims == nil {
		return errors.New("Access to endpoint not allowed: no claims found in Bearer token")
	}

	if !bitbucketAllowedRegexp.MatchString(r.URL.Path) {
		return errors.New("Access to endpoint not allowed: this part of BitBucket's API has been restricted")
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

func rewriteBitBucketLink(link, endpointAPIURL, proxyAPIURL string) string {
	return proxyAPIURL + strings.TrimPrefix(link, endpointAPIURL)
}

func rewriteLinksInBitBucketResponse(resp *http.Response, endpointAPIURL, proxyAPIURL string) error {
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var b map[string]interface{}
	if err := json.Unmarshal(body, &b); err != nil {
		return err
	}

	if next, ok := b["next"].(string); ok {
		b["next"] = rewriteBitBucketLink(next, endpointAPIURL, proxyAPIURL)
	}

	if prev, ok := b["previous"].(string); ok {
		b["previous"] = rewriteBitBucketLink(prev, endpointAPIURL, proxyAPIURL)
	}

	newBody, err := json.Marshal(b)
	println(newBody)
	resp.Body = ioutil.NopCloser(bytes.NewReader(newBody))

	return nil
}

type BitBucketTransport struct{}

func (t *BitBucketTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	ctx := r.Context()
	config := getConfig(ctx)
	resp, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		return resp, err
	}

	// remove CORS headers from BitBucket and use our own
	resp.Header.Del("Access-Control-Allow-Origin")

	if strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		repo := config.BitBucket.Repo
		apiURL := singleJoiningSlash(config.BitBucket.Endpoint, "/repositories/"+repo)
		err = rewriteLinksInBitBucketResponse(resp, apiURL, "")
		if err != nil {
			return resp, err
		}
	}

	return resp, nil
}
