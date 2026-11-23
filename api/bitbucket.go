package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
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
	proxy         *httputil.ReverseProxy
	oauthEndpoint oauth2.Endpoint

	tokenSourcesMu sync.Mutex
	tokenSources   map[string]*bitbucketTokenSource
}

func NewBitBucketGateway() *BitBucketGateway {
	return &BitBucketGateway{
		proxy: &httputil.ReverseProxy{
			Director:     bitbucketDirector,
			Transport:    &BitBucketTransport{},
			ErrorHandler: proxyErrorHandler,
		},
		oauthEndpoint: bitbucket.Endpoint,
		tokenSources:  map[string]*bitbucketTokenSource{},
	}
}

var bitbucketPathRegexp = regexp.MustCompile("^/bitbucket/?")
var bitbucketAllowedRegexp = regexp.MustCompile("^/bitbucket/src/?")

var bitbucketTokenExpirationMessageRegexp = regexp.MustCompile("(?i)^access token expired")

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

type bitbucketCredentials struct {
	refreshToken string
	clientID     string
	clientSecret string
}

type bitbucketTokenSource struct {
	credentials bitbucketCredentials
	oauth2.TokenSource
}

// tokenSource returns the token source for the current request's instance ID
// (retrieved from the specified context).
func (bb *BitBucketGateway) tokenSource(ctx context.Context) oauth2.TokenSource {
	config := getConfig(ctx)
	instanceID := getInstanceID(ctx)
	credentials := bitbucketCredentials{
		refreshToken: config.BitBucket.RefreshToken,
		clientID:     config.BitBucket.ClientID,
		clientSecret: config.BitBucket.ClientSecret,
	}

	bb.tokenSourcesMu.Lock()
	defer bb.tokenSourcesMu.Unlock()

	// Cache invalidation: Only use cached token source if the instance's
	// credentials have not been rotated since the source was cached.
	if cached, ok := bb.tokenSources[instanceID]; ok && cached.credentials == credentials {
		return cached
	}

	blankToken := &oauth2.Token{
		AccessToken:  "",
		RefreshToken: credentials.refreshToken,
	}
	oauthConfig := &oauth2.Config{
		ClientID:     credentials.clientID,
		ClientSecret: credentials.clientSecret,
		Endpoint:     bb.oauthEndpoint,
	}
	tokenSource := &bitbucketTokenSource{
		credentials: credentials,
		TokenSource: &notifyRefreshTokenSource{
			// oauth2 will use the specified context for future token refreshes,
			// so don't give it a request-scoped context.
			new: oauthConfig.TokenSource(context.Background(), blankToken),
			t:   blankToken,
		},
	}
	bb.tokenSources[instanceID] = tokenSource
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

	tokenSource := bb.tokenSource(ctx)
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
	var bodyReader io.ReadCloser
	var err error
	switch resp.Header.Get("Content-Encoding") {
	case "gzip":
		bodyReader, err = gzip.NewReader(resp.Body)
		if err != nil {
			return nil
		}
		defer bodyReader.Close()
	default:
		bodyReader = resp.Body
	}
	body, err := ioutil.ReadAll(bodyReader)
	if err != nil {
		return nil
	}

	var b map[string]interface{}
	if err := json.Unmarshal(body, &b); err != nil {
		return nil
	}

	if next, ok := b["next"].(string); ok {
		b["next"] = rewriteBitBucketLink(next, endpointAPIURL, proxyAPIURL)
	}

	if prev, ok := b["previous"].(string); ok {
		b["previous"] = rewriteBitBucketLink(prev, endpointAPIURL, proxyAPIURL)
	}

	newBodyBytes, err := json.Marshal(b)

	switch resp.Header.Get("Content-Encoding") {
	case "gzip":
		var compressedBody bytes.Buffer
		w := gzip.NewWriter(&compressedBody)
		defer w.Close()
		_, err = w.Write(newBodyBytes)
		if err != nil {
			return err
		}
		resp.Body = ioutil.NopCloser(&compressedBody)
	default:
		resp.Body = ioutil.NopCloser(bytes.NewReader(newBodyBytes))
	}

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
