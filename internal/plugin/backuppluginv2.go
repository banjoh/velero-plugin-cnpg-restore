package plugin

import (
	"context"
	"sort"
	"time"

	"github.com/sirupsen/logrus"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/pkg/errors"
	v1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	"github.com/vmware-tanzu/velero/pkg/plugin/velero"
)

const (
	// AnnotationServerName is the annotation key used to store the CNPG server name
	// for restore operations to reference the backup source
	AnnotationServerName = "velero-cnpg/serverName"

	// AnnotationCurrentBackupID is the annotation key used to store the backup ID
	// from the latest completed CNPG backup for precise point-in-time recovery
	AnnotationCurrentBackupID = "velero-cnpg/current-backup-id"
)

// BackupPluginV2 is a v2 backup item action plugin for Velero.
type BackupPluginV2 struct {
	log logrus.FieldLogger
}

// NewBackupPluginV2 instantiates a v2 BackupPlugin.
func NewBackupPluginV2(log logrus.FieldLogger) *BackupPluginV2 {
	return &BackupPluginV2{log: log}
}

// Name is required to implement the interface, but the Velero pod does not delegate this
// method -- it's used to tell velero what name it was registered under. The plugin implementation
// must define it, but it will never actually be called.
func (p *BackupPluginV2) Name() string {
	return "cnpgBackupPlugin"
}

// AppliesTo returns information about which resources this action should be invoked for.
// The IncludedResources and ExcludedResources slices can include both resources
// and resources with group names. These work: "ingresses", "ingresses.extensions".
// A BackupPlugin's Execute function will only be invoked on items that match the returned
// selector. A zero-valued ResourceSelector matches all resources.
func (p *BackupPluginV2) AppliesTo() (velero.ResourceSelector, error) {
	return velero.ResourceSelector{
		IncludedResources: []string{"clusters.postgresql.cnpg.io"},
	}, nil
}

// extractPluginParameters extracts serverName from .spec.plugins[].parameters
func (p *BackupPluginV2) extractPluginParameters(itemContent map[string]interface{}) (serverName string, err error) {
	spec, found, err := unstructured.NestedFieldNoCopy(itemContent, "spec")
	if err != nil {
		return "", errors.Wrap(err, "failed to get spec field")
	}
	if !found {
		return "", errors.New("spec field not found")
	}

	specMap, ok := spec.(map[string]interface{})
	if !ok {
		return "", errors.New("spec is not a map")
	}

	plugins, found := specMap["plugins"]
	if !found {
		p.log.Info("No plugins found in spec")
		return "", nil
	}

	pluginsList, ok := plugins.([]interface{})
	if !ok {
		return "", errors.New("plugins is not a list")
	}

	for _, plugin := range pluginsList {
		pluginMap, ok := plugin.(map[string]interface{})
		if !ok {
			continue
		}

		parameters, found := pluginMap["parameters"]
		if !found {
			continue
		}

		paramsMap, ok := parameters.(map[string]interface{})
		if !ok {
			continue
		}

		if sn, found := paramsMap["serverName"]; found {
			if snStr, ok := sn.(string); ok {
				serverName = snStr
				break
			}
		}
	}

	return serverName, nil
}

// addAnnotation adds an annotation to the item's metadata
func (p *BackupPluginV2) addAnnotation(itemContent map[string]interface{}, key, value string) error {
	metadata, found, err := unstructured.NestedFieldNoCopy(itemContent, "metadata")
	if err != nil {
		return errors.Wrap(err, "failed to get metadata field")
	}
	if !found {
		return errors.New("metadata field not found")
	}

	metadataMap, ok := metadata.(map[string]interface{})
	if !ok {
		return errors.New("metadata is not a map")
	}

	annotations, found := metadataMap["annotations"]
	if !found {
		annotations = make(map[string]interface{})
	}

	annotationsMap, ok := annotations.(map[string]interface{})
	if !ok {
		return errors.New("annotations is not a map")
	}

	annotationsMap[key] = value
	metadataMap["annotations"] = annotationsMap

	if err := unstructured.SetNestedField(itemContent, metadataMap, "metadata"); err != nil {
		return errors.Wrap(err, "failed to set metadata field")
	}

	return nil
}

// getLatestCompletedBackupID queries the Kubernetes API for the latest completed backup
// for the specified cluster and returns its backupId from status
func (p *BackupPluginV2) getLatestCompletedBackupID(ctx context.Context, namespace, clusterName string) (string, error) {
	// Get dynamic client for querying CRDs
	dynamicClient, err := GetDynamicClient()
	if err != nil {
		return "", errors.Wrap(err, "failed to create dynamic client")
	}

	// Define the GVR for CNPG Backup resources
	gvr := schema.GroupVersionResource{
		Group:    "postgresql.cnpg.io",
		Version:  "v1",
		Resource: "backups",
	}

	// List all backup resources in the namespace
	backupList, err := dynamicClient.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", errors.Wrap(err, "failed to list CNPG backup resources")
	}

	if len(backupList.Items) == 0 {
		p.log.Warnf("No backup resources found in namespace %s", namespace)
		return "", nil
	}

	// Filter and collect completed backups for this cluster
	var completedBackups []unstructured.Unstructured
	for _, backup := range backupList.Items {
		// Check if backup belongs to this cluster
		spec, found, err := unstructured.NestedFieldNoCopy(backup.Object, "spec")
		if err != nil || !found {
			continue
		}
		specMap, ok := spec.(map[string]interface{})
		if !ok {
			continue
		}

		// Check cluster reference
		cluster, found := specMap["cluster"]
		if !found {
			continue
		}
		clusterMap, ok := cluster.(map[string]interface{})
		if !ok {
			continue
		}
		backupClusterName, found := clusterMap["name"]
		if !found {
			continue
		}
		if backupClusterName != clusterName {
			continue
		}

		// Check if backup is completed
		status, found, err := unstructured.NestedFieldNoCopy(backup.Object, "status")
		if err != nil || !found {
			continue
		}
		statusMap, ok := status.(map[string]interface{})
		if !ok {
			continue
		}

		phase, found := statusMap["phase"]
		if !found {
			continue
		}
		phaseStr, ok := phase.(string)
		if !ok || phaseStr != "completed" {
			continue
		}

		completedBackups = append(completedBackups, backup)
	}

	if len(completedBackups) == 0 {
		p.log.Warnf("No completed backups found for cluster %s in namespace %s", clusterName, namespace)
		return "", nil
	}

	// Sort by creation timestamp (descending - newest first)
	sort.Slice(completedBackups, func(i, j int) bool {
		timeI := completedBackups[i].GetCreationTimestamp()
		timeJ := completedBackups[j].GetCreationTimestamp()
		return timeI.After(timeJ.Time)
	})

	// Extract backupId from the latest backup
	latestBackup := completedBackups[0]
	status, found, err := unstructured.NestedFieldNoCopy(latestBackup.Object, "status")
	if err != nil || !found {
		return "", errors.New("failed to get status from latest backup")
	}
	statusMap, ok := status.(map[string]interface{})
	if !ok {
		return "", errors.New("status is not a map in latest backup")
	}

	backupID, found := statusMap["backupId"]
	if !found {
		return "", errors.New("backupId not found in latest backup status")
	}
	backupIDStr, ok := backupID.(string)
	if !ok {
		return "", errors.New("backupId is not a string")
	}

	p.log.Infof("Found latest completed backup: %s with backupId: %s", latestBackup.GetName(), backupIDStr)
	return backupIDStr, nil
}

// Execute allows the ItemAction to perform arbitrary logic with the item being backed up
func (p *BackupPluginV2) Execute(item runtime.Unstructured, backup *v1.Backup) (runtime.Unstructured, []velero.ResourceIdentifier, string, []velero.ResourceIdentifier, error) {
	p.log.Info("Executing CNPG backup plugin on resource: %s", resourceName(item))

	itemContent := item.UnstructuredContent()

	// Extract serverName from .spec.plugins[].parameters
	serverName, err := p.extractPluginParameters(itemContent)
	if err != nil {
		return nil, nil, "", nil, err
	}

	if serverName == "" {
		p.log.Info("No serverName found in plugins.parameters, skipping annotation")
		return item, nil, "", nil, nil
	}

	// Add annotation with the extracted serverName
	p.log.Infof("Found serverName: %s", serverName)
	if err := p.addAnnotation(itemContent, AnnotationServerName, serverName); err != nil {
		return nil, nil, "", nil, err
	}

	// Get cluster metadata for backup query
	metadata, found, err := unstructured.NestedFieldNoCopy(itemContent, "metadata")
	if err == nil && found {
		metadataMap, ok := metadata.(map[string]interface{})
		if ok {
			namespace, _ := metadataMap["namespace"].(string)
			clusterName, _ := metadataMap["name"].(string)

			if namespace != "" && clusterName != "" {
				// Query for latest completed backup with timeout
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()

				backupID, err := p.getLatestCompletedBackupID(ctx, namespace, clusterName)
				if err != nil {
					p.log.Warnf("Failed to get latest backup ID: %v", err)
				} else if backupID != "" {
					if err := p.addAnnotation(itemContent, AnnotationCurrentBackupID, backupID); err != nil {
						p.log.Warnf("Failed to annotate backup ID: %v", err)
					} else {
						p.log.Infof("Annotated cluster with backup ID: %s", backupID)
					}
				} else {
					p.log.Warn("No completed backups found for cluster")
				}
			}
		}
	}

	item.SetUnstructuredContent(itemContent)
	p.log.Infof("Successfully annotated cluster (serverName: %s)", serverName)

	return item, nil, "", nil, nil
}

func (p *BackupPluginV2) Progress(operationID string, backup *v1.Backup) (velero.OperationProgress, error) {
	progress := velero.OperationProgress{}
	return progress, nil
}

func (p *BackupPluginV2) Cancel(operationID string, backup *v1.Backup) error {
	return nil
}

func resourceName(item runtime.Unstructured) string {
	metadata, found, err := unstructured.NestedFieldNoCopy(item.UnstructuredContent(), "metadata")
	if err != nil {
		return ""
	}
	if !found {
		return ""
	}

	metadataMap, ok := metadata.(map[string]interface{})
	if !ok {
		return ""
	}

	return metadataMap["name"].(string)
}
