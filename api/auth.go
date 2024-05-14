package api

import (
	"context"
	"crypto/rsa"
	"io/ioutil"
	"net/http"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/sirupsen/logrus"
)

// requireAuthentication checks incoming requests for tokens presented using the Authorization header
func (a *API) requireAuthentication(w http.ResponseWriter, r *http.Request) (context.Context, error) {
	logrus.Info("Getting auth token")
	token, err := a.extractBearerToken(w, r)
	if err != nil {
		return nil, err
	}

	logrus.Infof("Parsing JWT claims: %v", token)
	return a.parseJWTClaims(token, r)
}

func (a *API) extractBearerToken(w http.ResponseWriter, r *http.Request) (string, error) {
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

func (a *API) parseJWTClaims(bearer string, r *http.Request) (context.Context, error) {
	config := getConfig(r.Context())
	p := jwt.Parser{ValidMethods: []string{jwt.SigningMethodHS256.Name, jwt.SigningMethodRS256.Name}}
	token, err := p.ParseWithClaims(bearer, &GatewayClaims{}, func(token *jwt.Token) (interface{}, error) {
		signMethod := config.JWT.Method
		if signMethod == "" {
			signMethod = "HS256" // don't break backwards compatibility
		}

		switch signMethod {
		case "HS256":
			return []byte(config.JWT.Secret), nil
		case "RS256":
			return loadRSAPublicKeyFromDisk(config.JWT.Keyfile), nil
		default:
			return nil, unauthorizedError("Invalid Signing Method: %s", signMethod)
		}
	})
	if err != nil {
		return nil, unauthorizedError("Invalid token: %v", err)
	}

	return withToken(r.Context(), token), nil
}

func loadRSAPublicKeyFromDisk(location string) *rsa.PublicKey {
	keyData, e := ioutil.ReadFile(location)
	if e != nil {
		panic(e.Error())
	}
	key, e := jwt.ParseRSAPublicKeyFromPEM(keyData)
	if e != nil {
		panic(e.Error())
	}
	return key
}
