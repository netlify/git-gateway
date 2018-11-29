package api

import (
	"context"
	"net/http"
	"regexp"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/go-chi/chi"
	"github.com/netlify/git-gateway/conf"
	"github.com/netlify/git-gateway/storage"
	"github.com/netlify/git-gateway/storage/dial"
	"github.com/netlify/netlify-commons/graceful"
	"github.com/rs/cors"
	"github.com/sebest/xff"
	"github.com/sirupsen/logrus"
)

const (
	audHeaderName  = "X-JWT-AUD"
	defaultVersion = "unknown version"
)

var bearerRegexp = regexp.MustCompile(`^(?:B|b)earer (\S+$)`)

// API is the main REST API
type API struct {
	handler http.Handler
	db      storage.Connection
	auth    Auth
	config  *conf.GlobalConfiguration
	version string
}

type GatewayClaims struct {
	jwt.StandardClaims
	Email        string                 `json:"email"`
	AppMetaData  map[string]interface{} `json:"app_metadata"`
	UserMetaData map[string]interface{} `json:"user_metadata"`
}

// ListenAndServe starts the REST API
func (a *API) ListenAndServe(hostAndPort string) {
	log := logrus.WithField("component", "api")
	server := graceful.NewGracefulServer(a.handler, log)
	if err := server.Bind(hostAndPort); err != nil {
		log.WithError(err).Fatal("http server bind failed")
	}

	if err := server.Listen(); err != nil {
		log.WithError(err).Fatal("http server listen failed")
	}
}

// NewAPI instantiates a new REST API
func NewAPI(globalConfig *conf.GlobalConfiguration, db storage.Connection) *API {
	return NewAPIWithVersion(context.Background(), globalConfig, db, defaultVersion)
}

// NewAPIWithVersion creates a new REST API using the specified version
func NewAPIWithVersion(ctx context.Context, globalConfig *conf.GlobalConfiguration, db storage.Connection, version string) *API {
	auth := NewAuthWithVersion(ctx, version)
	api := &API{config: globalConfig, db: db, auth: *auth, version: version}

	xffmw, _ := xff.Default()

	r := newRouter()
	r.UseBypass(xffmw.Handler)
	r.Use(addRequestID)
	r.UseBypass(newStructuredLogger(logrus.StandardLogger()))
	r.Use(recoverer)

	r.Get("/health", api.HealthCheck)

	r.Route("/", func(r *router) {
		if globalConfig.MultiInstanceMode {
			r.Use(api.loadJWSSignatureHeader)
			r.Use(api.loadInstanceConfig)
		}
		r.With(api.auth.accessControl).Mount("/github", NewGitHubGateway())
		r.With(api.auth.accessControl).Mount("/gitlab", NewGitLabGateway())
		r.With(api.auth.accessControl).Mount("/bitbucket", NewBitBucketGateway())
		r.With(api.auth.accessControl).Get("/settings", api.Settings)
	})

	if globalConfig.MultiInstanceMode {
		// Operator microservice API
		r.With(api.verifyOperatorRequest).Get("/", api.GetAppManifest)
		r.Route("/instances", func(r *router) {
			r.Use(api.verifyOperatorRequest)

			r.Post("/", api.CreateInstance)
			r.Route("/{instance_id}", func(r *router) {
				r.Use(api.loadInstance)

				r.Get("/", api.GetInstance)
				r.Put("/", api.UpdateInstance)
				r.Delete("/", api.DeleteInstance)
			})
		})
	}

	corsHandler := cors.New(cors.Options{
		AllowedMethods:   []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch},
		AllowedHeaders:   []string{"Accept", "Authorization", "Private-Token", "Content-Type", audHeaderName},
		AllowCredentials: true,
	})

	api.handler = corsHandler.Handler(chi.ServerBaseContext(r, ctx))
	return api
}

// NewAPIFromConfigFile creates a new REST API using the provided configuration file.
func NewAPIFromConfigFile(filename string, version string) (*API, error) {
	globalConfig, err := conf.LoadGlobal(filename)
	if err != nil {
		return nil, err
	}
	config, err := conf.LoadConfig(filename)
	if err != nil {
		return nil, err
	}

	db, err := dial.Dial(globalConfig)
	if err != nil {
		return nil, err
	}

	logrus.Infof("Config is: %v", config)
	ctx, err := WithInstanceConfig(context.Background(), config, "")
	if err != nil {
		logrus.Fatalf("Error loading instance config: %+v", err)
	}

	return NewAPIWithVersion(ctx, globalConfig, db, version), nil
}

func (a *API) HealthCheck(w http.ResponseWriter, r *http.Request) error {
	return sendJSON(w, http.StatusOK, map[string]string{
		"version":     a.version,
		"name":        "git-gateway",
		"description": "git-gateway is a user registration and authentication API",
	})
}

func WithInstanceConfig(ctx context.Context, config *conf.Configuration, instanceID string) (context.Context, error) {
	ctx = withConfig(ctx, config)
	ctx = withInstanceID(ctx, instanceID)

	return ctx, nil
}
