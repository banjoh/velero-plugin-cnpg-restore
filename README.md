# Velero Plugin for CNPG Cluster Backup and Restore

A Velero plugin that enables automated backup and restore of CloudNativePG (CNPG) clusters with point-in-time recovery capabilities. This plugin integrates CNPG's Barman backup system with Velero's disaster recovery workflows.

## Overview

This plugin provides three specialized Velero plugins:

1. **CNPG Backup Plugin** - Captures cluster metadata and backup IDs during Velero backup operations
2. **CNPG Restore Plugin** - Configures cluster recovery from Barman backups during Velero restore operations
3. **Deployment Restore Plugin** - Removes migration-specific init containers during restore

## How It Works

### Backup Flow

When backing up a CNPG cluster, the **Backup Plugin** (`replicated.com/cnpg-backup-plugin`):

1. **Captures Backup Source Configuration**
   - Extracts the `serverName` from `.spec.plugins[].parameters` in the Cluster CR
   - Annotates the Cluster CR with `velero-cnpg/serverName` for restore reference

2. **Queries Latest Backup ID**
   - Lists all CNPG Backup resources in the cluster's namespace
   - Filters for completed backups belonging to the cluster
   - Sorts by creation timestamp to find the most recent backup
   - Extracts the `backupId` from the backup's status

3. **Annotates Cluster CR**
   - Adds `velero-cnpg/current-backup-id` annotation with the latest backup ID
   - This enables precise point-in-time recovery during restore

**Annotations Added:**
```yaml
metadata:
  annotations:
    velero-cnpg/serverName: "original-cluster-name"
    velero-cnpg/current-backup-id: "20241024T123456"
```

### Restore Flow

When restoring a CNPG cluster, the **Restore Plugin** (`replicated.com/cnpg-restore-plugin`):

1. **Validates Backup Metadata**
   - Checks for `velero-cnpg/serverName` annotation (backup source)
   - Retrieves optional `velero-cnpg/current-backup-id` for point-in-time recovery
   - Extracts `barmanObjectName` from `.spec.plugins[].parameters`

2. **Generates New Server Identity**
   - Creates unique `serverName` for restored cluster: `{clusterName}-{timestamp}`
   - Prevents backup conflicts between original and restored clusters
   - Example: `my-cluster-20241024-150405`

3. **Creates Configuration ConfigMap**
   - Generates `cnpg-velero-override` ConfigMap in cluster namespace
   - Stores mapping between old and new server names:
     ```yaml
     data:
       write_to_server_name: "my-cluster-20241024-150405"  # New identity
       read_from_server_name: "original-cluster-name"       # Backup source
     ```
   - **Purpose**: Enables Helm chart templates to dynamically reference the correct server names during future updates
   - **Helm Integration**: ConfigMap values can be referenced in chart templates to maintain consistency across upgrades:
     ```yaml
     # Example Helm template usage
     serverName: {{ .Values.global.cnpgServerName | default (index (lookup "v1" "ConfigMap" .Release.Namespace "cnpg-velero-override").data "write_to_server_name") }}
     ```
   - Annotated with `helm.sh/resource-policy: keep` to prevent deletion during Helm operations

4. **Configures External Cluster Reference**
   - Adds `.spec.externalClusters` configuration pointing to backup source:
     ```yaml
     spec:
       externalClusters:
       - name: clusterBackup
         plugin:
           name: barman-cloud.cloudnative-pg.io
           parameters:
             barmanObjectName: <storage-config>
             serverName: <original-server-name>
     ```

5. **Configures Bootstrap Recovery**
   - Replaces `.spec.bootstrap` with recovery configuration:
     ```yaml
     spec:
       bootstrap:
         recovery:
           source: clusterBackup
           recoveryTarget:
             backupID: "20241024T123456"  # Optional: point-in-time recovery
     ```

6. **Updates Plugin ServerName**
   - Updates `.spec.plugins[].parameters.serverName` to new unique value
   - Ensures new backups use the new server identity

7. **Removes Ephemeral Fields**
   - Cleans `status`, `resourceVersion`, `uid`, `generation`, `creationTimestamp`, `managedFields`
   - Ensures clean restoration without conflicts

### Deployment Restore Flow

The **Deployment Restore Plugin** (`replicated.com/deployment-restore-plugin`):

1. **Scans Deployments During Restore**
   - Inspects `.spec.template.spec.initContainers` in Deployment resources

2. **Removes Migration Init Containers**
   - Identifies init containers named `wait-for-migration-job`
   - Removes these containers from the restored deployment
   - These containers are specific to migration workflows and not needed in restored environments

3. **Cleans Up Empty Init Container Lists**
   - If all init containers are removed, deletes the entire `initContainers` field
   - Maintains clean deployment specifications

## Architecture

### Plugin Registration

The plugin registers three Velero plugins in [main.go](main.go:10-14):

```go
framework.NewServer().
    RegisterRestoreItemActionV2("replicated.com/cnpg-restore-plugin", newRestorePluginV2).
    RegisterRestoreItemActionV2("replicated.com/deployment-restore-plugin", newDeploymentRestorePlugin).
    RegisterBackupItemActionV2("replicated.com/cnpg-backup-plugin", newBackupPluginV2).
    Serve()
```

### Resource Selectors

- **Backup Plugin**: Applies to `clusters.postgresql.cnpg.io`
- **Restore Plugin**: Applies to `clusters.postgresql.cnpg.io`
- **Deployment Restore Plugin**: Applies to `deployments`

### Key Components

#### BackupPluginV2 ([backuppluginv2.go](internal/plugin/backuppluginv2.go))

- **extractPluginParameters**: Parses `serverName` from cluster spec
- **addAnnotation**: Adds annotations to cluster CR metadata
- **getLatestCompletedBackupID**: Queries Kubernetes API for latest completed backup
- **Execute**: Main backup logic orchestration

#### RestorePluginV2 ([restorepluginv2.go](internal/plugin/restorepluginv2.go))

- **getAnnotation**: Retrieves backup metadata from annotations
- **generateNewServerName**: Creates unique identity for restored cluster
- **createOrUpdateConfigMap**: Generates server name mapping ConfigMap
- **configureExternalCluster**: Sets up backup source reference
- **configureBootstrapRecovery**: Configures recovery with optional backup ID
- **updatePluginServerName**: Updates plugin configuration for new identity
- **removeEphemeralFields**: Cleans cluster CR for restoration
- **Execute**: Main restore logic orchestration

#### DeploymentRestorePlugin ([deploymentrestoreplugin.go](internal/plugin/deploymentrestoreplugin.go))

- **Execute**: Filters and removes migration init containers