package backtor

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/sirupsen/logrus"
)

func InitDB() (*sql.DB, error) {
	// prometheus.MustRegister(metricsSQLCounter)

	db0, err := sql.Open("sqlite3", fmt.Sprintf("%s/sqlite.db", opt.DataDir))
	if err != nil {
		return nil, err
	}

	statement, err1 := db0.Prepare("CREATE TABLE IF NOT EXISTS backup_spec (name TEXT, enabled INTEGER NOT NULL, running_create_workflow TEXT, from_date TIMESTAMP, to_date TIMESTAMP, last_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, backup_cron_string TEXT NOT NULL, retention_minutely VARCHAR NOT NULL DEFAULT '0@L', retention_hourly VARCHAR NOT NULL DEFAULT '0@L', retention_daily VARCHAR NOT NULL DEFAULT '4@L', retention_weekly VARCHAR NOT NULL DEFAULT '4@L', retention_monthly VARCHAR NOT NULL DEFAULT '3@L', retention_yearly VARCHAR NOT NULL DEFAULT '2@L', PRIMARY KEY(`name`))")
	if err1 != nil {
		return nil, err1
	}
	_, err1 = statement.Exec()
	if err1 != nil {
		return nil, err1
	}

	statement, err1 = db0.Prepare("CREATE TABLE IF NOT EXISTS materialized_backup (id TEXT NOT NULL, backup_name TEXT NOT NULL, data_id TEXT, status TEXT NOT NULL, running_delete_workflow TEXT, start_time TIMESTAMP NOT NULL, end_time TIMESTAMP NOT NULL, size REAL, minutely INTEGER NOT NULL DEFAULT 0, hourly INTEGER NOT NULL DEFAULT 0, daily INTEGER NOT NULL DEFAULT 0, weekly INTEGER NOT NULL DEFAULT 0, monthly INTEGER NOT NULL DEFAULT 0, yearly INTEGER NOT NULL DEFAULT 0, reference INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(`id`))")
	if err1 != nil {
		return nil, err1
	}
	_, err1 = statement.Exec()
	if err1 != nil {
		return nil, err1
	}

	os.MkdirAll(opt.DataDir, os.ModePerm)

	logrus.Debug("Database initialized")
	return db0, nil
}
