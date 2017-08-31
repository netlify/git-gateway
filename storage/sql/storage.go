package sql

import (
	// this is where we do the connections

	"net/url"

	// import drivers we might need
	_ "github.com/GoogleCloudPlatform/cloudsql-proxy/proxy/dialers/mysql"
	_ "github.com/GoogleCloudPlatform/cloudsql-proxy/proxy/dialers/postgres"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"

	"github.com/jinzhu/gorm"
	"github.com/netlify/git-gateway/conf"
	"github.com/netlify/git-gateway/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type logger struct {
	entry *logrus.Entry
}

func (l logger) Print(v ...interface{}) {
	l.entry.Print(v...)
}

// Connection represents a sql connection.
type Connection struct {
	db *gorm.DB
}

// Automigrate creates any missing tables and/or columns.
func (conn *Connection) Automigrate() error {
	conn.db = conn.db.AutoMigrate(&models.Instance{})
	return conn.db.Error
}

// Close closes the database connection.
func (conn *Connection) Close() error {
	return conn.db.Close()
}

// GetInstance finds an instance by ID
func (conn *Connection) GetInstance(instanceID string) (*models.Instance, error) {
	instance := models.Instance{}
	if rsp := conn.db.Where("id = ?", instanceID).First(&instance); rsp.Error != nil {
		if rsp.RecordNotFound() {
			return nil, models.InstanceNotFoundError{}
		}
		return nil, errors.Wrap(rsp.Error, "error finding instance")
	}
	return &instance, nil
}

func (conn *Connection) GetInstanceByUUID(uuid string) (*models.Instance, error) {
	instance := models.Instance{}
	if rsp := conn.db.Where("uuid = ?", uuid).First(&instance); rsp.Error != nil {
		if rsp.RecordNotFound() {
			return nil, models.InstanceNotFoundError{}
		}
		return nil, errors.Wrap(rsp.Error, "error finding instance")
	}
	return &instance, nil
}

func (conn *Connection) CreateInstance(instance *models.Instance) error {
	if result := conn.db.Create(instance); result.Error != nil {
		return errors.Wrap(result.Error, "Error creating instance")
	}
	return nil
}

func (conn *Connection) UpdateInstance(instance *models.Instance) error {
	if result := conn.db.Save(instance); result.Error != nil {
		return errors.Wrap(result.Error, "Error updating instance record")
	}
	return nil
}

func (conn *Connection) DeleteInstance(instance *models.Instance) error {
	return conn.db.Delete(instance).Error
}

// Dial will connect to that storage engine
func Dial(config *conf.GlobalConfiguration) (*Connection, error) {
	if config.DB.Driver == "" && config.DB.URL != "" {
		u, err := url.Parse(config.DB.URL)
		if err != nil {
			return nil, errors.Wrap(err, "parsing db connection url")
		}
		config.DB.Driver = u.Scheme
	}

	if config.DB.Dialect == "" {
		config.DB.Dialect = config.DB.Driver
	}
	db, err := gorm.Open(config.DB.Dialect, config.DB.Driver, config.DB.URL)
	if err != nil {
		return nil, errors.Wrap(err, "opening database connection")
	}

	if err := db.DB().Ping(); err != nil {
		return nil, errors.Wrap(err, "checking database connection")
	}

	db.SetLogger(logger{logrus.WithField("db-connection", config.DB.Driver)})

	if logrus.StandardLogger().Level == logrus.DebugLevel {
		db.LogMode(true)
	}

	conn := &Connection{
		db: db,
	}

	return conn, nil
}
