package dial

import (
	"github.com/netlify/git-gateway/conf"
	"github.com/netlify/git-gateway/models"
	"github.com/netlify/git-gateway/storage"
	"github.com/netlify/git-gateway/storage/sql"
)

// Dial will connect to that storage engine
func Dial(config *conf.GlobalConfiguration) (storage.Connection, error) {
	if config.DB.Namespace != "" {
		models.Namespace = config.DB.Namespace
	}

	var conn storage.Connection
	var err error
	conn, err = sql.Dial(config)

	if err != nil {
		return nil, err
	}

	if config.DB.Automigrate {
		if err := conn.Automigrate(); err != nil {
			conn.Close()
			return nil, err
		}
	}

	return conn, nil
}
