package api

import (
	"context"
	"net/http"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/sirupsen/logrus"
	"github.com/okta/okta-jwt-verifier-golang"
)

type Authenticator interface {
	// `authenticate` checks incoming requests for tokens presented using the Authorization header
	authenticate(w http.ResponseWriter, r *http.Request) (context.Context, error)
	getName() string
}

type Authorizer interface {
	// `authorize` checks incoming requests for roles data in tokens that is parsed and verified by a prior `authenticate` step
	authorize(w http.ResponseWriter, r *http.Request) (context.Context, error)
	getName() string
}

type Auth struct {
	authenticator Authenticator
	authorizer Authorizer
	version string
}

type JWTAuthenticator struct {
	name string
	auth Auth
}

type OktaJWTAuthenticator struct {
	name string
	auth Auth
}

type RolesAuthorizer struct {
	name string
	auth Auth
}

func NewAuthWithVersion(ctx context.Context, version string) *Auth {
	config := getConfig(ctx)
	auth := &Auth{version: version}
	authenticatorName := config.JWT.Authenticator

	if (authenticatorName == "bearer-jwt-token") {
		auth.authenticator = &JWTAuthenticator{name: "bearer-jwt-token", auth: *auth}
	} else if (authenticatorName == "bearer-okta-jwt-token") {
		auth.authenticator = &OktaJWTAuthenticator{name: "bearer-okta-jwt-token", auth: *auth}
	} else {
		if (authenticatorName != "") {
			logrus.Fatal("Authenticator `%v` is not recognized", authenticatorName)
		} else {
			logrus.Fatal("Authenticator is not defined")
		}
	}

	auth.authorizer = &RolesAuthorizer{name: "bearer-jwt-token-roles", auth: *auth}

	return auth
}

// check both authentication and authorization
func (a *Auth) accessControl(w http.ResponseWriter, r *http.Request) (context.Context, error) {
	logrus.Infof("Authenticate with: %v", a.authenticator.getName())
	ctx, err := a.authenticator.authenticate(w, r)
	if err != nil {
		return nil, err
	}

	logrus.Infof("Authorizing with: %v", a.authorizer.getName())
	return a.authorizer.authorize(w, r.WithContext(ctx))
}

func (a *Auth) extractBearerToken(w http.ResponseWriter, r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", unauthorizedError("This endpoint requires a Bearer token")
	}

	matches := bearerRegexp.FindStringSubmatch(authHeader)
	if len(matches) != 2 {
		return "", unauthorizedError("This endpoint requires a Bearer token")
	}

	return matches[1], nil
}

func (a *JWTAuthenticator) getName() string {
	return a.name
}

func (a *JWTAuthenticator) authenticate(w http.ResponseWriter, r *http.Request) (context.Context, error) {
	logrus.Info("Getting auth token")
	token, err := a.auth.extractBearerToken(w, r)
	if err != nil {
		return nil, err
	}

	logrus.Infof("Parsing JWT claims: %v", token)
	return a.parseJWTClaims(token, r)
}

func (a *JWTAuthenticator) parseJWTClaims(bearer string, r *http.Request) (context.Context, error) {
	config := getConfig(r.Context())
	p := jwt.Parser{ValidMethods: []string{jwt.SigningMethodHS256.Name}}
	token, err := p.ParseWithClaims(bearer, &GatewayClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(config.JWT.Secret), nil
	})

	if err != nil {
		return nil, unauthorizedError("Invalid token: %v", err)
	}
	claims := token.Claims.(GatewayClaims)
	return withClaims(r.Context(), &claims), nil
}

func (a *OktaJWTAuthenticator) getName() string {
	return a.name
}

func (a *OktaJWTAuthenticator) authenticate(w http.ResponseWriter, r *http.Request) (context.Context, error) {
	logrus.Info("Getting auth token")
	token, err := a.auth.extractBearerToken(w, r)
	if err != nil {
		return nil, err
	}

	logrus.Infof("Parsing JWT claims: %v", token)
	return a.parseOktaJWTClaims(token, r)
}

func (a *OktaJWTAuthenticator) parseOktaJWTClaims(bearer string, r *http.Request) (context.Context, error) {
	config := getConfig(r.Context())

	toValidate := map[string]string{}
	toValidate["aud"] = config.JWT.AUD
	toValidate["cid"] = config.JWT.CID

	jwtVerifierSetup := jwtverifier.JwtVerifier{
		Issuer: config.JWT.Issuer,
		ClaimsToValidate: toValidate,
	}

	verifier := jwtVerifierSetup.New()

	token, err := verifier.VerifyAccessToken(bearer)

	if err != nil {
		return nil, unauthorizedError("Invalid token: %v", err)
	}
	logrus.Infof("parseJWTClaims passed")

	claims := GatewayClaims{
		Email: token.Claims["sub"].(string),
		AppMetaData: nil,
		UserMetaData: nil,
		StandardClaims: jwt.StandardClaims{
			Audience: 	token.Claims["aud"].(string),
			ExpiresAt:  int64(token.Claims["exp"].(float64)),
			Id:			token.Claims["jti"].(string),
			IssuedAt:	int64(token.Claims["iat"].(float64)),
			Issuer:		token.Claims["iss"].(string),
			NotBefore:	0,
			Subject:	token.Claims["sub"].(string),
		},
	}

	return withClaims(r.Context(), &claims), nil
}

func (a *RolesAuthorizer) getName() string {
	return a.name
}

func (a *RolesAuthorizer) authorize(w http.ResponseWriter, r *http.Request) (context.Context, error) {
	ctx := r.Context()
	claims := getClaims(ctx)
	config := getConfig(ctx)

	logrus.Infof("authenticate url: %v+", r.URL)
	logrus.Infof("claims: %v+", claims)
	if claims == nil {
		return nil, unauthorizedError("Access to endpoint not allowed: no claims found in Bearer token")
	}

	if len(config.Roles) == 0 {
		return ctx, nil
	}

	roles, ok := claims.AppMetaData["roles"]
	if ok {
		roleStrings, _ := roles.([]interface{})
		for _, data := range roleStrings {
			role, _ := data.(string)
			for _, adminRole := range config.Roles {
				if role == adminRole {
					return ctx, nil
				}
			}
		}
	}

	return nil, unauthorizedError("Access to endpoint not allowed: your role doesn't allow access")
}
