package conf

import (
	"os"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
	"github.com/netlify/netlify-commons/nconf"
)

const DefaultGitHubEndpoint = "https://api.github.com"
const DefaultGitLabEndpoint = "https://gitlab.com/api/v4"
const DefaultGitLabTokenType = "oauth"
const DefaultBitBucketEndpoint = "https://api.bitbucket.org/2.0"

type GitHubConfig struct {
	AccessToken string `envconfig:"ACCESS_TOKEN" json:"access_token,omitempty"`
	Endpoint    string `envconfig:"ENDPOINT" json:"endpoint"`
	Repo        string `envconfig:"REPO" json:"repo"` // Should be "owner/repo" format
}

type GitLabConfig struct {
	AccessToken     string `envconfig:"ACCESS_TOKEN" json:"access_token,omitempty"`
	AccessTokenType string `envconfig:"ACCESS_TOKEN_TYPE" json:"access_token_type"`
	Endpoint        string `envconfig:"ENDPOINT" json:"endpoint"`
	Repo            string `envconfig:"REPO" json:"repo"` // Should be "owner/repo" format
}

type BitBucketConfig struct {
	RefreshToken string `envconfig:"REFRESH_TOKEN" json:"refresh_token,omitempty"`
	ClientID     string `envconfig:"CLIENT_ID" json:"client_id,omitempty"`
	ClientSecret string `envconfig:"CLIENT_SECRET" json:"client_secret,omitempty"`
	Endpoint     string `envconfig:"ENDPOINT" json:"endpoint"`
	Repo         string `envconfig:"REPO" json:"repo"`
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
	CID    string `envconfig:"CLIENT_ID" json:"client_id,omitempty"`
	Issuer string `envconfig:"ISSUER" json:"issuer,omitempty"`
	AUD    string `envconfig:"AUD" json:"aud,omitempty"`
	Authenticator string `envconfig:"AUTHENTICATOR" json:"authenticator,omitempty"`
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
	JWT       JWTConfiguration `json:"jwt"`
	GitHub    GitHubConfig     `envconfig:"GITHUB" json:"github"`
	GitLab    GitLabConfig     `envconfig:"GITLAB" json:"gitlab"`
	BitBucket BitBucketConfig  `envconfig:"BITBUCKET" json:"bitbucket"`
	Roles     []string         `envconfig:"ROLES" json:"roles"`
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
	if config.GitHub.Endpoint == "" {
		config.GitHub.Endpoint = DefaultGitHubEndpoint
	}
	if config.GitLab.Endpoint == "" {
		config.GitLab.Endpoint = DefaultGitLabEndpoint
	}
	if config.GitLab.AccessTokenType == "" {
		config.GitLab.AccessTokenType = DefaultGitLabTokenType
	}
	if config.BitBucket.Endpoint == "" {
		config.BitBucket.Endpoint = DefaultBitBucketEndpoint
	}
}
