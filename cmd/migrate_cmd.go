package cmd

import (
	"github.com/netlify/git-gateway/conf"
	"github.com/netlify/git-gateway/storage/dial"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var migrateCmd = cobra.Command{
	Use:  "migrate",
	Long: "Migrate database structures. This will create new tables and add missing columns and indexes.",
	Run: func(cmd *cobra.Command, args []string) {
		execWithConfig(cmd, migrate)
	},
}

func migrate(globalConfig *conf.GlobalConfiguration, config *conf.Configuration) {
	db, err := dial.Dial(globalConfig)
	if err != nil {
		logrus.Fatalf("Error opening database: %+v", err)
	}

	defer db.Close()

	err = db.Automigrate()
	if err != nil {
		logrus.Fatalf("Error automigrating database: %+v", err)
	}

	logrus.Info("Automigration successful")

}
