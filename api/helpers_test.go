package api

import (
	"testing"

	"github.com/netlify/git-gateway/conf"
	"github.com/netlify/git-gateway/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitizeOutput(t *testing.T) {
	t.Run("InstanceResponse", func(t *testing.T) {
		v := InstanceResponse{
			Instance: models.Instance{
				BaseConfig: &conf.Configuration{
					GitHub: conf.GitHubConfig{
						AccessToken: "remove",
					},
				},
			},
		}

		ov := sanitizeOutput(v)
		require.IsType(t, v, ov)
		assert.Equal(t, "", ov.(InstanceResponse).Instance.BaseConfig.GitHub.AccessToken)
	})

	t.Run("InstanceResponsePtr", func(t *testing.T) {
		v := &InstanceResponse{
			Instance: models.Instance{
				BaseConfig: &conf.Configuration{
					GitHub: conf.GitHubConfig{
						AccessToken: "remove",
					},
				},
			},
		}

		ov := sanitizeOutput(v)
		require.IsType(t, v, ov)
		assert.Equal(t, "", ov.(*InstanceResponse).Instance.BaseConfig.GitHub.AccessToken)
	})

	t.Run("Instance", func(t *testing.T) {
		v := models.Instance{
			BaseConfig: &conf.Configuration{
				GitHub: conf.GitHubConfig{
					AccessToken: "remove",
				},
			},
		}

		ov := sanitizeOutput(v)
		require.IsType(t, v, ov)
		assert.Equal(t, "", ov.(models.Instance).BaseConfig.GitHub.AccessToken)
	})

	t.Run("InstancePtr", func(t *testing.T) {
		v := &models.Instance{
			BaseConfig: &conf.Configuration{
				GitHub: conf.GitHubConfig{
					AccessToken: "remove",
				},
			},
		}

		ov := sanitizeOutput(v)
		require.IsType(t, v, ov)
		assert.Equal(t, "", ov.(*models.Instance).BaseConfig.GitHub.AccessToken)
	})

	t.Run("Configuration", func(t *testing.T) {
		v := conf.Configuration{
			GitHub: conf.GitHubConfig{
				AccessToken: "remove",
			},
		}

		ov := sanitizeOutput(v)
		require.IsType(t, v, ov)
		assert.Equal(t, "", ov.(conf.Configuration).GitHub.AccessToken)
	})

	t.Run("ConfigurationPtr", func(t *testing.T) {
		v := &conf.Configuration{
			GitHub: conf.GitHubConfig{
				AccessToken: "remove",
			},
		}

		ov := sanitizeOutput(v)
		require.IsType(t, v, ov)
		assert.Equal(t, "", ov.(*conf.Configuration).GitHub.AccessToken)
	})
}
