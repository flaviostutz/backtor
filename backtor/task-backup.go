package backtor

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

var (
	metricsInitialized = false
)

//METRICS
var backupLastSizeGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name: "backtor_backup_last_size_mbytes",
	Help: "Last successful backup size in bytes",
}, []string{
	"backup",
})

var backupLastTimeGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name: "backtor_backup_last_time_seconds",
	Help: "Last successful backup time",
}, []string{
	"backup",
})

var backupTasksCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "backtor_backup_tasks_total",
	Help: "Total backup tasks triggered",
}, []string{
	"backup",
	"status",
})

var backupTriggerCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "backtor_backup_trigger_total",
	Help: "Total backups triggered",
}, []string{
	"backup",
	"status",
})

var backupMaterializedCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "backtor_workflow_total",
	Help: "Total backup workflows",
}, []string{
	"backup",
	"status",
})

var backupTagCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "backtor_backup_tag_total",
	Help: "Total backups that were tagged",
}, []string{
	"backup",
	"status",
})

var overallBackupWarnCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "backtor_backup_warn_total",
	Help: "Total overall backup warnings",
}, []string{
	"backup",
	"status",
})

func InitTaskBackup() {
	prometheus.MustRegister(backupLastSizeGauge)
	prometheus.MustRegister(backupLastTimeGauge)
	prometheus.MustRegister(backupTasksCounter)
	prometheus.MustRegister(backupMaterializedCounter)
	prometheus.MustRegister(backupTagCounter)
	prometheus.MustRegister(overallBackupWarnCounter)
}

func triggerNewBackup(backupName string) (workflowID string, err3 error) {
	start := time.Now()
	logrus.Info("")
	logrus.Info(">>>> BACKUP WORKFLOW LAUNCH %s", backupName)

	logrus.Debugf("Checking if there is another backup running. name=%s", backupName)

	bs, err := getBackupSpec(backupName)
	if err != nil {
		return "", fmt.Errorf("Couldn't load backup spec. err=%s", err)
	}

	if bs.RunningCreateWorkflowID != nil {
		wf, err := getWorkflowInstance(*bs.RunningCreateWorkflowID)
		logrus.Debugf("Workflow %v", wf)
		if err != nil {
			if wf.status != "NOT_FOUND" {
				return "", fmt.Errorf("Couldn't get workflow id %s for checking if it is running. backup name %s. err=%s", *bs.RunningCreateWorkflowID, backupName, err)
			}
			logrus.Warnf("Workflow %s is set to backup spec, but was not found in Conductor. Proceeding to create a new workflow instance. backup=%s", backupName, *bs.RunningCreateWorkflowID)
		}
		if wf.status == "RUNNING" {
			overallBackupWarnCounter.WithLabelValues(backupName, "warning").Inc()
			return "", fmt.Errorf("Another backup workflow for backup %s is running (%s)", backupName, wf.workflowID)
		}
	}

	logrus.Debugf("Launching workflow for backup creation. api=%s", opt.ConductorAPIURL)
	workflowID, err1 := launchCreateBackupWorkflow(backupName)
	if err1 != nil {
		overallBackupWarnCounter.WithLabelValues(backupName, "error").Inc()
		return "", fmt.Errorf("Couldn't invoke Conductor workflow for backup creation. err=%s", err1)
	}

	logrus.Infof("Workflow launched successfuly. workflowID=%s", workflowID)
	err4 := updateBackupSpecRunningCreateWorkflowID(backupName, &workflowID)
	if err4 != nil {
		return "", err4
	}

	elapsed := time.Now().Sub(start)
	logrus.Debugf("Backup triggering done. elapsed=%s", elapsed)
	return workflowID, nil
}

func checkBackupWorkflow(backupName string) {
	logrus.Debugf("checkBackupTask %s", backupName)
	bs, err := getBackupSpec(backupName)
	if err != nil {
		logrus.Debugf("Couldn't get backup spec %s. err=%s", backupName, err)
		overallBackupWarnCounter.WithLabelValues(backupName, "error").Inc()
	}
	if bs.RunningCreateWorkflowID == nil {
		logrus.Debugf("Backup Spec %s has no running workflow set", backupName)
		return
	}
	wf, err0 := getWorkflowInstance(*bs.RunningCreateWorkflowID)
	if err0 != nil {
		logrus.Debugf("Couldn't get workflow instance %s. err=%s", *bs.RunningCreateWorkflowID, err0)
		overallBackupWarnCounter.WithLabelValues(backupName, "error").Inc()
		return
	}

	if wf.status == "RUNNING" {
		logrus.Debugf("Workflow %s was launched for backup %s and is still running", wf.workflowID, backupName)
		return
	}

	logrus.Infof("Conductor workflow id %s finish detected. status=%s. backup=%s", wf.workflowID, wf.status, backupName)
	//avoid doing retention until the newly created backup is tagged to avoid it to be elected for removal (because it will have no tags)
	avoidRetentionLock.Lock()
	defer avoidRetentionLock.Unlock()

	err2 := updateBackupSpecRunningCreateWorkflowID(backupName, nil)
	if err2 != nil {
		logrus.Errorf("Couldn't set backup spec running create workflowid to nil. err=%s", err2)
		overallBackupWarnCounter.WithLabelValues(backupName, "error").Inc()
		return
	}

	if wf.status != "COMPLETED" {
		logrus.Warnf("Workflow %s completed with status!=COMPLETED. backupName=%s. status=%s", wf.workflowID, backupName, wf.status)
		overallBackupWarnCounter.WithLabelValues(backupName, "warning").Inc()
		return
	}

	if wf.dataID == nil || wf.dataSizeMB == nil || *wf.dataSizeMB == 0 {
		logrus.Warnf("Workflow %s has completed but didn't return dataID and dataSizeMB. Check worker. Backup will be ignored. workflow=%v", wf.workflowID, wf)
		overallBackupWarnCounter.WithLabelValues(backupName, "warning").Inc()
		return
	}

	err1 := createMaterializedBackup(wf.workflowID, backupName, wf.dataID, wf.status, wf.startTime, wf.endTime, wf.dataSizeMB)
	if err1 != nil {
		logrus.Errorf("Couldn't create materialized backup on database. err=%s", err1)
		overallBackupWarnCounter.WithLabelValues(backupName, "error").Inc()
		return
	}

	logrus.Debugf("Materialized backup saved to database successfuly. id=%s", wf.workflowID)
	backupMaterializedCounter.WithLabelValues(backupName, "success").Inc()
	backupLastSizeGauge.WithLabelValues(backupName).Set(*wf.dataSizeMB)
	backupLastTimeGauge.WithLabelValues(backupName).Set(float64(wf.endTime.Sub(wf.startTime).Seconds()))

	err = tagAllBackups(backupName)
	if err != nil {
		logrus.Errorf("Error tagging backups. err=%s", err)
		overallBackupWarnCounter.WithLabelValues(backupName, "error").Inc()
	}
}

func tagAllBackups(backupName string) error {
	logrus.Debugf("Tagging backups")

	bs, err := getBackupSpec(backupName)
	if err != nil {
		return fmt.Errorf("Couldn't load backup spec. err=%s", err)
	}

	//begin transaction
	logrus.Debug("Begining db transaction")
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("Error begining db transaction. err=%s", err)
	}

	//check last backup
	logrus.Debug("Checking for backups available")
	backups, err1 := getMaterializedBackups(bs.Name, 1, "", "COMPLETED", false)
	if err1 != nil {
		tx.Rollback()
		return fmt.Errorf("Error getting last backup. err=%s", err)
	} else if len(backups) == 0 {
		logrus.Warnf("No backups found. Skipping tagging.")
		tx.Rollback()
		return nil
	}
	lastBackup := backups[0]

	logrus.Debug("Clearing all backup tags")
	res, err0 := clearTagsAndReferenceMaterializedBackup(tx)
	if err0 != nil {
		tx.Rollback()
		return fmt.Errorf("Error clearing tags. err=%s", err0)
	}
	logrus.Debugf("%d rows affected", mu(res.RowsAffected())[0])

	//minutely
	logrus.Debugf("Marking reference + minutely tags")
	res, err = markReferencesMinutelyMaterializedBackup(tx, bs.Name, bs.MinutelyParams()[1])
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("Error marking reference+minutely tags. err=%s", err)
	}
	logrus.Debugf("%d rows affected", mu(res.RowsAffected())[0])

	//hourly
	logrus.Debugf("Marking hourly tags")
	res, err = markTagMaterializedBackup(tx, "hourly", "minutely", "%Y-%m-%dT%H:0:0.000", "%M", bs.Name, bs.HourlyParams()[1])
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("Error marking hourly tags. err=%s", err)
	}
	logrus.Debugf("%d rows affected", mu(res.RowsAffected())[0])

	//daily
	logrus.Debugf("Marking daily tags")
	res, err = markTagMaterializedBackup(tx, "daily", "hourly", "%Y-%m-%w-%dT0:0:0.000", "%H", bs.Name, bs.DailyParams()[1])
	if err != nil {
		tx.Rollback()
		backupTagCounter.WithLabelValues(bs.Name, "error").Inc()
		return fmt.Errorf("Error marking daily tags. err=%s", err)
	}
	tc, _ := res.RowsAffected()
	logrus.Debugf("%d rows affected", tc)

	//weekly
	logrus.Debugf("Marking weekly tags")
	res, err = markTagMaterializedBackup(tx, "weekly", "daily", "%Y-%m-%W-0T0:0:0.000", "%w", bs.Name, bs.WeeklyParams()[1])
	if err != nil {
		tx.Rollback()
		backupTagCounter.WithLabelValues(bs.Name, "error").Inc()
		return fmt.Errorf("Error marking weekly tags. err=%s", err)
	}
	tc, _ = res.RowsAffected()
	logrus.Debugf("%d rows affected", tc)

	//monthly
	logrus.Debugf("Marking monthly tags")
	ref := bs.MonthlyParams()[1]
	if ref == "L" {
		ref = "31"
	}
	res, err = markTagMaterializedBackup(tx, bs.Name, "monthly", "daily", "%Y-%m-0T0:0:0.000", "%d", ref)
	if err != nil {
		tx.Rollback()
		backupTagCounter.WithLabelValues(bs.Name, "error").Inc()
		return fmt.Errorf("Error marking monthly tags. err=%s", err)
	}
	tc, _ = res.RowsAffected()
	logrus.Debugf("%d rows affected", tc)

	//yearly
	logrus.Debugf("Marking yearly tags")
	res, err = markTagMaterializedBackup(tx, "yearly", "monthly", "%Y-0-0T0:0:0.000", "%m", bs.Name, bs.YearlyParams()[1])
	if err != nil {
		tx.Rollback()
		backupTagCounter.WithLabelValues(bs.Name, "error").Inc()
		return fmt.Errorf("Error marking yearly tags. err=%s", err)
	}
	tc, _ = res.RowsAffected()
	logrus.Debugf("%d rows affected", tc)

	logrus.Debug("Tagging last backup with all tags")
	res, err = setAllTagsMaterializedBackup(tx, lastBackup.ID)
	if err != nil {
		tx.Rollback()
		backupTagCounter.WithLabelValues(bs.Name, "error").Inc()
		return fmt.Errorf("Error tagging last backup. err=%s", err)
	}
	tc, _ = res.RowsAffected()
	logrus.Debugf("%d rows affected", tc)

	logrus.Debug("Commiting transaction")
	err = tx.Commit()
	if err != nil {
		tx.Rollback()
		backupTagCounter.WithLabelValues(bs.Name, "error").Inc()
		return fmt.Errorf("Error commiting transation. err=%s", err)
	}
	backupTagCounter.WithLabelValues(bs.Name, "success").Inc()
	return nil
}

func mu(a ...interface{}) []interface{} {
	return a
}
