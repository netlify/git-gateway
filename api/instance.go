package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi"
	"github.com/netlify/git-gateway/conf"
	"github.com/netlify/git-gateway/models"
	"github.com/pborman/uuid"
)

func (a *API) loadInstance(w http.ResponseWriter, r *http.Request) (context.Context, error) {
	instanceID := chi.URLParam(r, "instance_id")
	logEntrySetField(r, "instance_id", instanceID)

	i, err := a.db.GetInstance(instanceID)
	if err != nil {
		if models.IsNotFoundError(err) {
			return nil, notFoundError("Instance not found")
		}
		return nil, internalServerError("Database error loading instance").WithInternalError(err)
	}

	return withInstance(r.Context(), i), nil
}

func (a *API) GetAppManifest(w http.ResponseWriter, r *http.Request) error {
	// TODO update to real manifest
	return sendJSON(w, http.StatusOK, map[string]string{
		"version":     a.version,
		"name":        "GitGateway",
		"description": "GitGateway is an access control proxy to git repos",
	})
}

type InstanceRequestParams struct {
	UUID       string              `json:"uuid"`
	BaseConfig *conf.Configuration `json:"config"`
}

type InstanceResponse struct {
	models.Instance
	Endpoint string `json:"endpoint"`
	State    string `json:"state"`
}

func (a *API) CreateInstance(w http.ResponseWriter, r *http.Request) error {
	params := InstanceRequestParams{}
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		return badRequestError("Error decoding params: %v", err)
	}

	_, err := a.db.GetInstanceByUUID(params.UUID)
	if err != nil {
		if !models.IsNotFoundError(err) {
			return internalServerError("Database error looking up instance").WithInternalError(err)
		}
	} else {
		return badRequestError("An instance with that UUID already exists")
	}

	i := models.Instance{
		ID:         uuid.NewRandom().String(),
		UUID:       params.UUID,
		BaseConfig: params.BaseConfig,
	}
	if err = a.db.CreateInstance(&i); err != nil {
		return internalServerError("Database error creating instance").WithInternalError(err)
	}

	resp := InstanceResponse{
		Instance: i,
		Endpoint: a.config.API.Endpoint,
		State:    "active",
	}
	return sendJSON(w, http.StatusCreated, resp)
}

func (a *API) GetInstance(w http.ResponseWriter, r *http.Request) error {
	i := getInstance(r.Context())
	return sendJSON(w, http.StatusOK, i)
}

func (a *API) UpdateInstance(w http.ResponseWriter, r *http.Request) error {
	i := getInstance(r.Context())

	params := InstanceRequestParams{}
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		return badRequestError("Error decoding params: %v", err)
	}

	if params.BaseConfig != nil {
		i.BaseConfig = mergeConfig(i.BaseConfig, params.BaseConfig)
	}

	if err := a.db.UpdateInstance(i); err != nil {
		return internalServerError("Database error updating instance").WithInternalError(err)
	}
	return sendJSON(w, http.StatusOK, i)
}

func (a *API) DeleteInstance(w http.ResponseWriter, r *http.Request) error {
	i := getInstance(r.Context())
	if err := a.db.DeleteInstance(i); err != nil {
		return internalServerError("Database error deleting instance").WithInternalError(err)
	}

	// TODO do we delete everything associated with an instance too?

	w.WriteHeader(http.StatusNoContent)
	return nil
}

func mergeConfig(baseConfig *conf.Configuration, newConfig *conf.Configuration) *conf.Configuration {
	if newConfig.GitHub.AccessToken != "" {
		baseConfig.GitHub.AccessToken = newConfig.GitHub.AccessToken
	}

	if newConfig.GitHub.Endpoint != "" {
		baseConfig.GitHub.Endpoint = newConfig.GitHub.Endpoint
	}

	if newConfig.GitHub.Repo != "" {
		baseConfig.GitHub.Repo = newConfig.GitHub.Repo
	}

	if newConfig.GitLab.AccessToken != "" {
		baseConfig.GitLab.AccessToken = newConfig.GitLab.AccessToken
	}

	if newConfig.GitLab.AccessTokenType != "" {
		baseConfig.GitLab.AccessTokenType = newConfig.GitLab.AccessTokenType
	}

	if newConfig.GitLab.Endpoint != "" {
		baseConfig.GitLab.Endpoint = newConfig.GitLab.Endpoint
	}

	if newConfig.GitLab.Repo != "" {
		baseConfig.GitLab.Repo = newConfig.GitLab.Repo
	}

	baseConfig.Roles = newConfig.Roles
	return baseConfig
}
