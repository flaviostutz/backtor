package backtor

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

//METRICS
var retentionTasksCounter = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "backtor_retention_tasks_total",
	Help: "Total retention tasks triggered",
})

var retentionBackupsDeleteCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "backtor_retention_backup_delete_total",
	Help: "Total retention backups deleted",
}, []string{
	"backup",
	"status",
})

var retentionBackupsRetriesCounter = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "backtor_retention_backup_delete_retries_total",
	Help: "Total retention backup delete retries",
})

// var avoidRetentionLock = &sync.Mutex{}
var retentionLocks = make(map[string]*sync.Mutex)

func retentionLock(backupName string) *sync.Mutex {
	m, ok := retentionLocks[backupName]
	if !ok {
		m = &sync.Mutex{}
		retentionLocks[backupName] = m
	}
	return m
}

func InitTaskRetention() {
	prometheus.MustRegister(retentionTasksCounter)
	prometheus.MustRegister(retentionBackupsDeleteCounter)
	prometheus.MustRegister(retentionBackupsRetriesCounter)
}

func RunRetentionTask(backupName string) {
	logrus.Info("")
	logrus.Info(">>>> RUN RETENTION TASK")

	//avoid doing retention until the newly created backup is tagged to avoid it to be elected for removal (because it will have no tags)
	retentionLock(backupName).Lock()
	defer retentionLock(backupName).Unlock()

	start := time.Now()
	retentionTasksCounter.Inc()

	bs, err := getBackupSpec(backupName)
	if err != nil {
		logrus.Errorf("Could not get backup spec for %s. err=%s", backupName, err)
		return
	}

	tagAllBackups(backupName)

	logrus.Debugf("Retention policy: minutely=%s, hourly=%s, daily=%s, weekly=%s, monthly=%s, yearly=%s", bs.MinutelyParams()[0], bs.HourlyParams()[0], bs.DailyParams()[0], bs.WeeklyParams()[0], bs.MonthlyParams()[0], bs.YearlyParams()[0])

	electedBackups := make([]MaterializedBackup, 0)
	electedBackups = appendElectedForTag(backupName, "", "0", electedBackups)
	electedBackups = appendElectedForTag(backupName, "minutely", bs.MinutelyParams()[0], electedBackups)
	electedBackups = appendElectedForTag(backupName, "hourly", bs.HourlyParams()[0], electedBackups)
	electedBackups = appendElectedForTag(backupName, "daily", bs.DailyParams()[0], electedBackups)
	electedBackups = appendElectedForTag(backupName, "weekly", bs.WeeklyParams()[0], electedBackups)
	electedBackups = appendElectedForTag(backupName, "monthly", bs.MonthlyParams()[0], electedBackups)
	electedBackups = appendElectedForTag(backupName, "yearly", bs.YearlyParams()[0], electedBackups)
	logrus.Infof("%d backups elected for deletion", len(electedBackups))

	for _, backup := range electedBackups {
		logrus.Debugf("Deleting backup '%s'...", backup.ID)

		err := triggerBackupDelete(backup.ID)
		if err != nil {
			logrus.Errorf("Couldn't trigger backup delete for materialized backup %s. err=%s", backup.ID, err)
			retentionBackupsDeleteCounter.WithLabelValues("error").Inc()
			continue
		}

		//give some breath to backed webhook
		// time.Sleep(1000 * time.Millisecond)
	}

	elapsed := time.Now().Sub(start)
	logrus.Infof("Retention management task done. elapsed=%s", elapsed)
}

func triggerBackupDelete(materializedID string) error {
	logrus.Debugf("triggerBackupDelete %s", materializedID)
	mb, err := getMaterializedBackup(materializedID)
	if err != nil {
		return fmt.Errorf("Couldn't load materized backup %s", materializedID)
	}

	if mb.Status != "COMPLETED" {
		return fmt.Errorf("Materialized backup %s cannot be deleted because its status is not 'completed'. status=%s", mb.ID, mb.Status)
	}

	if mb.RunningDeleteWorkflowID != nil {
		return fmt.Errorf("Materialized backup %s cannot be deleted because it already has a runningCreateWorkflowID set", mb.ID)
	}

	bs, err1 := getBackupSpec(mb.BackupName)
	if err1 != nil {
		return fmt.Errorf("Error getting backup spec %s. err=%s", mb.BackupName, err1)
	}

	workflowID, err1 := launchRemoveBackupWorkflow(mb.BackupName, mb.DataID, bs.TimeoutSeconds, bs.WorkerConfig)
	if err1 != nil {
		overallBackupWarnCounter.WithLabelValues(mb.BackupName, "error").Inc()
		m := fmt.Sprintf("Couldn't invoke Conductor workflow for backup removal. err=%s", err1)
		return fmt.Errorf(m)
	}
	logrus.Infof("Backup %s delete workflow launched successfuly for dataID %s. workflowID=%s", mb.BackupName, mb.DataID, workflowID)

	_, err3 := setStatusMaterializedBackup(materializedID, "deleting", &workflowID)
	if err3 != nil {
		return fmt.Errorf("Couldn't update status of materialized backup %s to 'deleting'. err=%s", mb.ID, err3)
	}

	return nil
}

func checkWorkflowBackupRemove(backupName string) {
	logrus.Debugf("checkWorkflowBackupRemove backupName=%s", backupName)

	mbs, err := getMaterializedBackups(backupName, 20, "", "deleting", false)
	if err != nil {
		logrus.Warnf("Couldn't load materializeds for backup %s", backupName)
		overallBackupWarnCounter.WithLabelValues(backupName, "error").Inc()
		return
	}
	if len(mbs) == 0 {
		logrus.Debugf("No materialized backups pending delete for backup %s", backupName)
		return
	}

	for _, mb := range mbs {
		if mb.RunningDeleteWorkflowID == nil {
			logrus.Errorf("Materialized backup %s has no running delete workflow set but status is 'deleting'", mb.ID)
			overallBackupWarnCounter.WithLabelValues(backupName, "error").Inc()
			continue
		}

		logrus.Debugf("Checking backup %s. dataId=%s. deleteWorkflowId=%s", mb.BackupName, mb.DataID, *mb.RunningDeleteWorkflowID)

		relaunch := false
		wf, err0 := getWorkflowInstance(*mb.RunningDeleteWorkflowID)
		if err0 != nil {
			logrus.Debugf("Couldn't get workflow instance %s. err=%s", *mb.RunningDeleteWorkflowID, err0)
			overallBackupWarnCounter.WithLabelValues(backupName, "error").Inc()
			relaunch = true
		}
		logrus.Debugf("Found workflowId=%s. status=%s. relaunch=%s", wf.workflowID, wf.status, relaunch)

		if relaunch {
			logrus.Warnf("Materialized backup %s has status 'deleting' but there is something wrong with its workflow. Relaunching", mb.ID)
			bs, err1 := getBackupSpec(mb.BackupName)
			if err1 != nil {
				logrus.Errorf("Error getting backup spec %s. err=%s", mb.BackupName, err1)
				continue
			}
			wid, err2 := launchRemoveBackupWorkflow(mb.BackupName, mb.DataID, bs.TimeoutSeconds, bs.WorkerConfig)
			if err2 != nil {
				logrus.Warnf("Couldn't relaunch workflow for deleting dataId %s. err=%s", mb.DataID, err2)
				continue
			}
			logrus.Infof("Workflow relaunched to delete dataId %s. workflowId=%s", mb.DataID, wid)
			continue
		}

		if wf.status == "RUNNING" {
			logrus.Debugf("Workflow %s for removing materialized backup is still running", *mb.RunningDeleteWorkflowID)
			continue
		}

		logrus.Infof("Conductor workflow %s for backup deletion of %s has finished. status=%s", wf.workflowID, backupName, wf.status)

		if wf.status != "COMPLETED" {
			logrus.Warnf("Workflow %s has finished but status is not COMPLETED. status=%s. backupName=%s. dataId=%s", wf.workflowID, wf.status, mb.BackupName, mb.DataID)
			_, err2 := setStatusMaterializedBackup(mb.ID, "delete-error", mb.RunningDeleteWorkflowID)
			if err2 != nil {
				logrus.Errorf("Couldn't set materialized backup status. err=%s", err2)
				overallBackupWarnCounter.WithLabelValues(backupName, "error").Inc()
				continue
			}
		}

		logrus.Warnf("Workflow %s has finished. status=%s. backupName=%s. dataId=%s", wf.workflowID, wf.status, mb.BackupName, mb.DataID)
		_, err2 := setStatusMaterializedBackup(mb.ID, "deleted", nil)
		if err2 != nil {
			logrus.Errorf("Couldn't set materialized backup status. err=%s", err2)
			overallBackupWarnCounter.WithLabelValues(backupName, "error").Inc()
			continue
		}
		logrus.Warnf("Workflow %s has completed and backup was removed. dataId=%s. backupName=%s", wf.workflowID, mb.DataID, mb.BackupName)
		retentionBackupsDeleteCounter.WithLabelValues(backupName, wf.status).Inc()
		continue
	}
}

func appendElectedForTag(backupName string, tag string, retentionCount string, appendTo []MaterializedBackup) []MaterializedBackup {
	ret, err0 := strconv.Atoi(retentionCount)
	if err0 != nil {
		logrus.Errorf("%s: Invalid retention parameter: err=%s", tag, err0)
		return appendTo
	}
	mbackups, err := getExclusiveTagAvailableMaterializedBackups(backupName, tag, ret, 30)
	if err != nil {
		logrus.Errorf("%s: Error querying backups for deletion. err=%s", tag, err)
		return appendTo
	}
	logrus.Debugf("%s: %d backups elected for deletion (limited to 10)", tag, len(mbackups))
	return append(appendTo, mbackups...)
}
