package storage

import "github.com/netlify/git-gateway/models"

// Connection is the interface a storage provider must implement.
type Connection interface {
	Close() error
	Automigrate() error

	GetInstanceByUUID(uuid string) (*models.Instance, error)
	GetInstance(instanceID string) (*models.Instance, error)
	CreateInstance(instance *models.Instance) error
	DeleteInstance(instance *models.Instance) error
	UpdateInstance(instance *models.Instance) error
}
