# backtor

Backtor is a backup scheduler tool that uses Conductor workers to handle backup operations.

It is focused on the scheduling part of a common backup routine, leaving the dirty storage job for specialized storage/database tools. You can use any backup backend by just implementing a simple Conductor worker for the tasks "backup" and "remove".

"backup" is called from time to time to create a new backup and "remove" task is launched for removing a previous backup that is not needed anymore, according to retention policy.

The triggering and retainment of backups are based on the functional perception of backups, so you configure:

   - Retention policies: for how long do a backup must be retained? It depends on what the user needs when something goes wrong. In general, the more recent, more backups in time you need. By default, Conductor will try to keep something like (if a backup is outside this, the "remove_backup" Workflow will be called):

       - the last 4 daily backups
       - the last 4 weekly backups
       - the last 3 monthly backups
       - the last 2 yearly backups

   - Triggering cron string: cron string that defines when a new backup will be created (some help on cron strings: https://crontab.guru/examples.html). If no cron string is provided, it will be derived from the need of the retention policy by default.

Based on those retention parameters, Backtor will launch a "create_backup" workflow or a "remove_backup" workflow on Conductor in order to maintain what we need as a backup that can save our souls! The actual backup creation or removal is performed by Conductor workers specialized on the target backup storage/tool.

Check out another Conductor based tool that may be helpful for you:
* [Schellar](https://github.com/flaviostutz/schellar) is a scheduler tool for instantiating Conductor workflows from time to time

Hope this can help you!

## Run

- copy docker-compose.yml

```yml
version: '3.5'

services:

  backtor:
    image: flaviostutz/backtor
    restart: always
    ports:
      - 6000:6000
    environment:
      - LOG_LEVEL=debug
      - CONDUCTOR_API_URL=http://backtor-conductor:8080/api

  backtor-conductor:
    image: flaviostutz/backtor-conductor
    restart: always
    ports:
      - 8080:8080
    environment:
      - DYNOMITE_HOSTS=dynomite:8102:us-east-1c
      - ELASTICSEARCH_URL=elasticsearch:9300
      - LOADSAMPLE=false
      - PROVISIONING_UPDATE_EXISTING_TASKS=false

  dynomite:
    image: flaviostutz/dynomite:0.7.5
    restart: always
    ports:
      - 8102:8102

  elasticsearch:
    image: docker.elastic.co/elasticsearch/elasticsearch:5.6.8
    restart: always
    environment:
      - "ES_JAVA_OPTS=-Xms512m -Xmx1000m"
      - transport.host=0.0.0.0
      - discovery.type=single-node
      - xpack.security.enabled=false
    ports:
      - 9200:9200
      - 9300:9300
    logging:
      driver: "json-file"
      options:
        max-size: "20MB"
        max-file: "5"

  conductor-ui:
    image: flaviostutz/conductor-ui
    restart: always
    environment:
      - WF_SERVER=http://backtor-conductor:8080/api/
    ports:
      - 5000:5000
```

- execute ```docker-compose up``` and see logs
- open Postman on your machine and import /tests/provisioning/collection.json
- execute the requests one by one
- observe what is happening on logs and at http://localhost:5000

## ENV configurations

- CONDUCTOR_API_URL - Netflix Conductor server URL
- DATA_DIR - data dir to create internal SQLITE database

## REST API

  - ```GET /backup```
    - Query backups managed by Backtor
    - Query params:
       - 'enabled' - 0 or 1

```json
[
    {
        "name": "backup72109432",
        "enabled": 1,
        "RunningCreateWorkflowID": "c0535ba5-f838-4de7-979b-f436a8a66b17",
        "backupCronString": "0/2 * * * * *",
        "lastUpdate": "2019-07-21T00:52:50.0846172Z",
        "retentionMinutely": "0@L",
        "retentionHourly": "0@L",
        "retentionDaily": "4@L",
        "retentionWeekly": "4@L",
        "retentionMonthly": "3@L",
        "retentionYearly": "2@L"
    }
]

  - ```POST /backup```
    - Create a new backup specification
    - Request body: json     
      ```json
        {
           name:{backup spec name},
           enabled:{0 or 1}
           fromDate:{iso date - from datetime to enable backup}
           toDate:{iso date - to datetime to enable backup}
           retentionHourly: {hourly policy}
           retentionDaily: {"4@L" means "keep 4 daily backups that are taken on the last hour (L) of the day"}
           retentionWeekly: {weekly policy}
           retentionMonthly: {monthly policy}
           retentionYearly: {yearly policy}
        }
      ```
      - status code must be 201

      - request json:
        - name - backup name
        - enabled - activate or not the tasks for this backup spec
        - runningCreateWorkflowID - set during workflow execution
        - backupCronString - schedule string that determines when backup (followed by retention jobs) will take place
        - lastUpdate - last time spec was updated
        - retentionMinutely - "[number of minutely backups to be retained]@[second to trigger backup]"
        - retentionHourly - "[number of hourly backups to be retained]@[minute to trigger backup]"
        - retentionDaily - "[number of daily backups to be retained]@[hour to trigger backup]"
        - retentionWeekly - "[number of weekly backups to be retained]@[weekday to trigger backup]"
        - retentionMonthly - "[number of monthly backups to be retained]@[day to trigger backup]"
        - retentionYearly - "[number of yearly backups to be retained]@[month to trigger backup]"
        - In all cases, "L" means "last unit of time", so if you use "2@L" for monthly retention it means "keep 2 monthly backups that are taken at the last day of the month"

  - ```PUT /backup```
    - Updates an existing backup specification
    - Request body: same as 'POST /backup'

  
#### Examples:

* Default backup
  * retentionMinutely   0@L
  * retentionHourly     0@L
  * retentionDaily      4@L
  * retentionWeekly     3@L
  * retentionMonthly    3@L
  * retentionYearly     2@L
  * Every day, at hour 0, minute 0, a daily backup will be triggered. Four of these backups will be kept.
  * At the last day of the week (SAT), the daily backup will be marked as a weekly backup. Three of these weekly backups will be kept.
  * At the last day of the month, the last hourly backup of the day will be marked as a monthly backup. Three of these monthly backups will be kept.
  * At the last month of the year, on the last day of the month, the daily backup will be marked as a yearly backup. Two of these labeled backups will be kept too.

* Simple daily backups
  * retentionDaily       7
  * The backup will be triggered every day at 23h59min (L) and 7 backups will be kept. On the 8th day, the first backup will be deleted

* Every 4 hours backups
  * backupCronString     0 0 */4 ? * *
  * retentionHourly      6
  * retentionDaily       0@3
  * retentionMonthly     2@L
  * Trigger a backup every 4 hours and keep 6 of them, deleting older ones.
  * Mark the backup created on the last day of the month near 3 am as 'monthly' and keep 2 of them.

## Conductor Workers

- In order to the backups to take place, you have to implement, or use a ready made Worker Conductor for the following tasks:

   - "backup"
    - perform actual backup creations
    - inputs:
      - backupName
      - workerConfig
    - output:
      - dataId - an Id that identifies the backup on target backup tool and will be used later to invoke backup removals when it is not neede anymore
      - dataSizeMB - the amount of data was backed up

   - "remove"
    - perform actual backup removals
    - inputs:
      - backupName
      - dataId
      - workerConfig

## Monitoring

Backtor has a /metrics endpoint compatible with Prometheus.

## Contribute

Please submit your issues and pull requests here!

## Some details

* Before trying to create a new backup, Backtor looks for "RUNNING" workflows on Conductor so that if there is another workflow running, it won't start a new one to avoid overwhelming long lasting backups (will skip it). For example, if there is a hourly backup active and the backup is taking 1h30 to complete, backups will be taken only from 2h to 2h hours.

