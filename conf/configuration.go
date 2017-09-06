package conf

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
	"github.com/netlify/netlify-commons/nconf"
)

const DefaultGitHubEndpoint = "https://api.github.com"

type GitHubConfig struct {
	AccessToken string `envconfig:"ACCESS_TOKEN" json:"access_token"`
	Endpoint    string `envconfig:"ENDPOINT" json:"endpoint"`
	Repo        string `envconfig:"REPO" json:"repo"` // Should be "owner/repo" format
}

// DBConfiguration holds all the database related configuration.
type DBConfiguration struct {
	Dialect     string `json:"dialect"`
	Driver      string `json:"driver" required:"true"`
	URL         string `json:"url" envconfig:"DATABASE_URL" required:"true"`
	Namespace   string `json:"namespace"`
	Automigrate bool   `json:"automigrate"`
}

// JWTConfiguration holds all the JWT related configuration.
type JWTConfiguration struct {
	Secret string `json:"secret" required:"true"`
}

// GlobalConfiguration holds all the configuration that applies to all instances.
type GlobalConfiguration struct {
	API struct {
		Host     string
		Port     int `envconfig:"PORT" default:"8081"`
		Endpoint string
	}
	DB                DBConfiguration
	Logging           nconf.LoggingConfig `envconfig:"LOG"`
	OperatorToken     string              `split_words:"true"`
	MultiInstanceMode bool
}

// Configuration holds all the per-instance configuration.
type Configuration struct {
	JWT    JWTConfiguration `json:"jwt"`
	GitHub GitHubConfig     `envconfig:"GITHUB" json:"github"`
	Roles  []string         `envconfig:"ROLES" json:"roles"`
}

func maskAccessToken(token string) string {
	return strings.Repeat("*", len(token))
}

func githubEndpoint(endpoint string) string {
	if endpoint == "" {
		return DefaultGitHubEndpoint
	}
	return endpoint
}

func (gh *GitHubConfig) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		AccessToken string `json:"access_token"`
		Endpoint    string `json:"endpoint"`
		Repo        string `json:"repo"`
	}{
		AccessToken: maskAccessToken(gh.AccessToken),
		Endpoint:    githubEndpoint(gh.Endpoint),
		Repo:        gh.Repo,
	})
}

func loadEnvironment(filename string) error {
	var err error
	if filename != "" {
		err = godotenv.Load(filename)
	} else {
		err = godotenv.Load()
		// handle if .env file does not exist, this is OK
		if os.IsNotExist(err) {
			return nil
		}
	}
	return err
}

// LoadGlobal loads configuration from file and environment variables.
func LoadGlobal(filename string) (*GlobalConfiguration, error) {
	if err := loadEnvironment(filename); err != nil {
		return nil, err
	}

	config := new(GlobalConfiguration)
	if err := envconfig.Process("gitgateway", config); err != nil {
		return nil, err
	}
	if _, err := nconf.ConfigureLogging(&config.Logging); err != nil {
		return nil, err
	}
	return config, nil
}

// LoadConfig loads per-instance configuration.
func LoadConfig(filename string) (*Configuration, error) {
	if err := loadEnvironment(filename); err != nil {
		return nil, err
	}

	config := new(Configuration)
	if err := envconfig.Process("gitgateway", config); err != nil {
		return nil, err
	}
	config.ApplyDefaults()
	return config, nil
}

// ApplyDefaults sets defaults for a Configuration
func (config *Configuration) ApplyDefaults() {
}
