package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/netlify/git-gateway/conf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type bitbucketTestEnv struct {
	gateway *BitBucketGateway
	api     *httptest.Server

	// tokenTTL is the expires_in value, in seconds, of issued access tokens.
	// oauth2 treats a token as expired 10 seconds early, so a value below 10
	// forces a refresh on every request.
	tokenTTL      int
	tokenRequests atomic.Int32
}

// newBitBucketTestEnv starts a fake Bitbucket API that echoes the bearer token
// it was called with, and a fake OAuth token endpoint that issues an access
// token naming the credentials it was asked to refresh.
func newBitBucketTestEnv(t *testing.T) *bitbucketTestEnv {
	env := &bitbucketTestEnv{tokenTTL: 3600}

	env.api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(r.Header.Get("Authorization")))
	}))
	t.Cleanup(env.api.Close)

	oauth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		env.tokenRequests.Add(1)
		clientID, clientSecret, ok := r.BasicAuth()
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": bitbucketTestAccessToken(conf.BitBucketConfig{
				RefreshToken: r.FormValue("refresh_token"),
				ClientID:     clientID,
				ClientSecret: clientSecret,
			}),
			"token_type": "bearer",
			"expires_in": env.tokenTTL,
		})
	}))
	t.Cleanup(oauth.Close)

	env.gateway = NewBitBucketGateway()
	env.gateway.oauthEndpoint.TokenURL = oauth.URL
	return env
}

func bitbucketTestAccessToken(c conf.BitBucketConfig) string {
	return "access/" + c.ClientID + "/" + c.ClientSecret + "/" + c.RefreshToken
}

func (env *bitbucketTestEnv) config(name string) *conf.Configuration {
	return &conf.Configuration{
		BitBucket: conf.BitBucketConfig{
			RefreshToken: name + "-refresh-token",
			ClientID:     name + "-client-id",
			ClientSecret: name + "-client-secret",
			Endpoint:     env.api.URL,
			Repo:         "owner/" + name,
		},
	}
}

// request sends a request through the gateway as instanceID and returns the
// Authorization header the Bitbucket API received.
func (env *bitbucketTestEnv) request(ctx context.Context, instanceID string, config *conf.Configuration) (int, string) {
	ctx = withConfig(ctx, config)
	ctx = withInstanceID(ctx, instanceID)
	ctx = withToken(ctx, &jwt.Token{Claims: &GatewayClaims{}})

	r := httptest.NewRequest(http.MethodGet, "/bitbucket/src/master/README.md", nil)
	w := httptest.NewRecorder()
	env.gateway.ServeHTTP(w, r.WithContext(ctx))
	return w.Code, w.Body.String()
}

func TestBitBucketGatewayTokenSource(t *testing.T) {
	t.Run("EachInstanceUsesItsOwnToken", func(t *testing.T) {
		env := newBitBucketTestEnv(t)
		configA, configB := env.config("a"), env.config("b")

		for i := 0; i < 2; i++ {
			code, auth := env.request(context.Background(), "instance-a", configA)
			require.Equal(t, http.StatusOK, code)
			assert.Equal(t, "Bearer "+bitbucketTestAccessToken(configA.BitBucket), auth)

			code, auth = env.request(context.Background(), "instance-b", configB)
			require.Equal(t, http.StatusOK, code)
			assert.Equal(t, "Bearer "+bitbucketTestAccessToken(configB.BitBucket), auth)
		}

		assert.EqualValues(t, 2, env.tokenRequests.Load(), "each instance's token should be fetched once and then reused")
	})

	t.Run("SingleInstanceMode", func(t *testing.T) {
		env := newBitBucketTestEnv(t)
		config := env.config("a")

		for i := 0; i < 2; i++ {
			code, auth := env.request(context.Background(), "", config)
			require.Equal(t, http.StatusOK, code)
			assert.Equal(t, "Bearer "+bitbucketTestAccessToken(config.BitBucket), auth)
		}

		assert.EqualValues(t, 1, env.tokenRequests.Load())
	})

	t.Run("RotatedCredentialsReplaceCachedToken", func(t *testing.T) {
		rotations := map[string]func(*conf.BitBucketConfig){
			"RefreshToken": func(c *conf.BitBucketConfig) { c.RefreshToken = "rotated-refresh-token" },
			"ClientID":     func(c *conf.BitBucketConfig) { c.ClientID = "rotated-client-id" },
			"ClientSecret": func(c *conf.BitBucketConfig) { c.ClientSecret = "rotated-client-secret" },
		}
		for name, rotate := range rotations {
			t.Run(name, func(t *testing.T) {
				env := newBitBucketTestEnv(t)
				before := env.config("a")
				after := env.config("a")
				rotate(&after.BitBucket)

				code, auth := env.request(context.Background(), "instance-a", before)
				require.Equal(t, http.StatusOK, code)
				assert.Equal(t, "Bearer "+bitbucketTestAccessToken(before.BitBucket), auth)

				code, auth = env.request(context.Background(), "instance-a", after)
				require.Equal(t, http.StatusOK, code)
				assert.Equal(t, "Bearer "+bitbucketTestAccessToken(after.BitBucket), auth)
			})
		}
	})

	t.Run("RefreshesAfterFirstRequestContextIsCancelled", func(t *testing.T) {
		env := newBitBucketTestEnv(t)
		env.tokenTTL = 1
		config := env.config("a")

		ctx, cancel := context.WithCancel(context.Background())
		code, _ := env.request(ctx, "instance-a", config)
		require.Equal(t, http.StatusOK, code)
		cancel()

		code, auth := env.request(context.Background(), "instance-a", config)
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, "Bearer "+bitbucketTestAccessToken(config.BitBucket), auth)
		assert.EqualValues(t, 2, env.tokenRequests.Load())
	})

	t.Run("ConcurrentFirstRequestsShareOneTokenSource", func(t *testing.T) {
		env := newBitBucketTestEnv(t)
		config := env.config("a")

		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				code, auth := env.request(context.Background(), "instance-a", config)
				assert.Equal(t, http.StatusOK, code)
				assert.Equal(t, "Bearer "+bitbucketTestAccessToken(config.BitBucket), auth)
			}()
		}
		wg.Wait()

		assert.EqualValues(t, 1, env.tokenRequests.Load())
	})
}
