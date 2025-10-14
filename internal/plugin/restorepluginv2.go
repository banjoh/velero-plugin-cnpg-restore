package plugin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	v1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	"github.com/vmware-tanzu/velero/pkg/plugin/velero"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// RestorePlugin is a restore item action plugin for Velero
type RestorePluginV2 struct {
	log logrus.FieldLogger
}

// NewRestorePluginV2 instantiates a v2 RestorePlugin.
func NewRestorePluginV2(log logrus.FieldLogger) *RestorePluginV2 {
	return &RestorePluginV2{log: log}
}

func GetClient() (*kubernetes.Clientset, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	clientConfig, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, errors.WithStack(err)
	}

	client, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return client, nil
}

// createOrUpdateConfigMap creates or updates the cnpg-velero-override ConfigMap
func (p *RestorePluginV2) createOrUpdateConfigMap(namespace, clusterName, writeServerName, readServerName string) error {
	client, err := GetClient()
	if err != nil {
		return errors.Wrap(err, "failed to get Kubernetes client")
	}

	configMapName := "cnpg-velero-override"

	// Create ConfigMap data
	configMapData := map[string]string{
		"write_to_server_name":  writeServerName,
		"read_from_server_name": readServerName,
	}

	// Try to create the ConfigMap
	_, err = client.CoreV1().ConfigMaps(namespace).Create(context.TODO(), &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: namespace,
			Annotations: map[string]string{
				"helm.sh/resource-policy": "keep",
			},
		},
		Data: configMapData,
	}, metav1.CreateOptions{})

	if err != nil {
		// If ConfigMap already exists, update it
		if strings.Contains(err.Error(), "already exists") {
			_, err = client.CoreV1().ConfigMaps(namespace).Update(context.TODO(), &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: namespace,
					Annotations: map[string]string{
						"helm.sh/resource-policy": "keep",
					},
				},
				Data: configMapData,
			}, metav1.UpdateOptions{})
			if err != nil {
				return errors.Wrap(err, "failed to update ConfigMap")
			}
			p.log.Infof("Updated ConfigMap %s/%s", namespace, configMapName)
		} else {
			return errors.Wrap(err, "failed to create ConfigMap")
		}
	} else {
		p.log.Infof("Created ConfigMap %s/%s", namespace, configMapName)
	}

	return nil
}

// extractBarmanObjectName extracts barmanObjectName from .spec.plugins[].parameters
func (p *RestorePluginV2) extractBarmanObjectName(itemContent map[string]interface{}) (string, error) {
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
		return "", errors.New("no plugins found in spec")
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

		if bon, found := paramsMap["barmanObjectName"]; found {
			if bonStr, ok := bon.(string); ok {
				return bonStr, nil
			}
		}
	}

	return "", errors.New("barmanObjectName not found in plugin parameters")
}

// Name is required to implement the interface, but the Velero pod does not delegate this
// method -- it's used to tell velero what name it was registered under. The plugin implementation
// must define it, but it will never actually be called.
func (p *RestorePluginV2) Name() string {
	return "cnpgRestorePlugin"
}

// AppliesTo returns information about which resources this action should be invoked for.
// The IncludedResources and ExcludedResources slices can include both resources
// and resources with group names. These work: "ingresses", "ingresses.extensions".
// A RestoreItemAction's Execute function will only be invoked on items that match the returned
// selector. A zero-valued ResourceSelector matches all resources.
func (p *RestorePluginV2) AppliesTo() (velero.ResourceSelector, error) {
	p.log.Info("RestorePluginV2.AppliesTo called")
	return velero.ResourceSelector{
		IncludedResources: []string{"clusters.postgresql.cnpg.io"},
	}, nil
}

// getAnnotation retrieves an annotation value from the item's metadata
func (p *RestorePluginV2) getAnnotation(itemContent map[string]interface{}, key string) (string, bool, error) {
	metadata, found, err := unstructured.NestedFieldNoCopy(itemContent, "metadata")
	if err != nil {
		return "", false, errors.Wrap(err, "failed to get metadata field")
	}
	if !found {
		return "", false, nil
	}

	metadataMap, ok := metadata.(map[string]interface{})
	if !ok {
		return "", false, errors.New("metadata is not a map")
	}

	annotations, found := metadataMap["annotations"]
	if !found {
		return "", false, nil
	}

	annotationsMap, ok := annotations.(map[string]interface{})
	if !ok {
		return "", false, errors.New("annotations is not a map")
	}

	value, found := annotationsMap[key]
	if !found {
		return "", false, nil
	}

	valueStr, ok := value.(string)
	if !ok {
		return "", false, errors.New("annotation value is not a string")
	}

	return valueStr, true, nil
}

// generateNewServerName creates a unique serverName using the cluster name and timestamp
func (p *RestorePluginV2) generateNewServerName(clusterName string) string {
	timestamp := time.Now().Format("20060102-150405")
	return fmt.Sprintf("%s-%s", clusterName, timestamp)
}

// removeEphemeralFields removes status and other ephemeral fields from the cluster CR
func (p *RestorePluginV2) removeEphemeralFields(itemContent map[string]interface{}) {
	// Remove status field
	delete(itemContent, "status")

	// Remove metadata fields that should not be restored
	if metadata, found := itemContent["metadata"]; found {
		if metadataMap, ok := metadata.(map[string]interface{}); ok {
			delete(metadataMap, "resourceVersion")
			delete(metadataMap, "uid")
			delete(metadataMap, "generation")
			delete(metadataMap, "creationTimestamp")
			delete(metadataMap, "managedFields")
		}
	}
}

// configureExternalCluster adds externalClusters configuration to the spec
func (p *RestorePluginV2) configureExternalCluster(itemContent map[string]interface{}, serverName, barmanObjectName string) error {
	spec, found, err := unstructured.NestedFieldNoCopy(itemContent, "spec")
	if err != nil {
		return errors.Wrap(err, "failed to get spec field")
	}
	if !found {
		return errors.New("spec field not found")
	}

	specMap, ok := spec.(map[string]interface{})
	if !ok {
		return errors.New("spec is not a map")
	}

	// Create externalClusters configuration
	externalClusters := []interface{}{
		map[string]interface{}{
			"name": "clusterBackup",
			"plugin": map[string]interface{}{
				"name": "barman-cloud.cloudnative-pg.io",
				"parameters": map[string]interface{}{
					"barmanObjectName": barmanObjectName,
					"serverName":       serverName,
				},
			},
		},
	}

	// Directly modify the spec map instead of using SetNestedField
	specMap["externalClusters"] = externalClusters

	return nil
}

// updatePluginServerName updates the serverName in spec.plugins[].parameters to a new unique value
func (p *RestorePluginV2) updatePluginServerName(itemContent map[string]interface{}, newServerName string) error {
	spec, found, err := unstructured.NestedFieldNoCopy(itemContent, "spec")
	if err != nil {
		return errors.Wrap(err, "failed to get spec field")
	}
	if !found {
		return errors.New("spec field not found")
	}

	specMap, ok := spec.(map[string]interface{})
	if !ok {
		return errors.New("spec is not a map")
	}

	plugins, found := specMap["plugins"]
	if !found {
		p.log.Info("No plugins found in spec, skipping serverName update")
		return nil
	}

	pluginsList, ok := plugins.([]interface{})
	if !ok {
		return errors.New("plugins is not a list")
	}

	// Update serverName in all plugins that have it
	updated := false
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

		// Update serverName if it exists
		if _, found := paramsMap["serverName"]; found {
			paramsMap["serverName"] = newServerName
			updated = true
			p.log.Infof("Updated plugin serverName to: %s", newServerName)
		}
	}

	if !updated {
		p.log.Warn("No serverName found in any plugin parameters")
	}

	return nil
}

// configureBootstrapRecovery updates bootstrap configuration to use recovery from backup
func (p *RestorePluginV2) configureBootstrapRecovery(itemContent map[string]interface{}) error {
	spec, found, err := unstructured.NestedFieldNoCopy(itemContent, "spec")
	if err != nil {
		return errors.Wrap(err, "failed to get spec field")
	}
	if !found {
		return errors.New("spec field not found")
	}

	specMap, ok := spec.(map[string]interface{})
	if !ok {
		return errors.New("spec is not a map")
	}

	// Replace bootstrap configuration with recovery
	bootstrap := map[string]interface{}{
		"recovery": map[string]interface{}{
			"source": "clusterBackup",
		},
	}

	// Directly modify the spec map
	specMap["bootstrap"] = bootstrap

	return nil
}

// Execute allows the RestorePlugin to perform arbitrary logic with the item being restored,
// in this case, configuring the cluster for recovery from backup.
func (p *RestorePluginV2) Execute(input *velero.RestoreItemActionExecuteInput) (*velero.RestoreItemActionExecuteOutput, error) {
	p.log.Info("Executing CNPG restore plugin on resource: %s", resourceName(input.Item))

	itemContent := input.Item.UnstructuredContent()

	// Check if this cluster was backed up with our plugin
	serverName, hasServerName, err := p.getAnnotation(itemContent, AnnotationServerName)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get serverName annotation")
	}

	if !hasServerName {
		p.log.Infof("No %s annotation found, skipping restore modifications", AnnotationServerName)
		out := velero.NewRestoreItemActionExecuteOutput(input.Item)
		return out, nil
	}

	p.log.Infof("Found serverName annotation: %s", serverName)

	// Extract barmanObjectName from .spec.plugins[].parameters
	barmanObjectName, err := p.extractBarmanObjectName(itemContent)
	if err != nil {
		return nil, errors.Wrap(err, "failed to extract barmanObjectName from plugin parameters")
	}

	p.log.Infof("Found barmanObjectName in plugin parameters: %s", barmanObjectName)

	// Get cluster name from metadata
	metadata, found, err := unstructured.NestedFieldNoCopy(itemContent, "metadata")
	if err != nil {
		return nil, errors.Wrap(err, "failed to get metadata for cluster name")
	}
	if !found {
		return nil, errors.New("metadata not found")
	}
	metadataMap, ok := metadata.(map[string]interface{})
	if !ok {
		return nil, errors.New("metadata is not a map")
	}
	clusterName, found := metadataMap["name"]
	if !found {
		return nil, errors.New("cluster name not found in metadata")
	}
	clusterNameStr, ok := clusterName.(string)
	if !ok {
		return nil, errors.New("cluster name is not a string")
	}

	// Generate new serverName for the restored cluster
	newServerName := p.generateNewServerName(clusterNameStr)
	p.log.Infof("Generated new serverName for restored cluster: %s (original: %s)", newServerName, serverName)

	// Create or update ConfigMap with serverName information
	namespace := metadataMap["namespace"].(string)
	if err := p.createOrUpdateConfigMap(namespace, clusterNameStr, newServerName, serverName); err != nil {
		return nil, errors.Wrap(err, "failed to create/update ConfigMap")
	}

	p.removeEphemeralFields(itemContent)

	// Update the plugin serverName to the new unique value
	if err := p.updatePluginServerName(itemContent, newServerName); err != nil {
		return nil, errors.Wrap(err, "failed to update plugin serverName")
	}
	p.log.Infof("Updated spec.plugins[].parameters.serverName to: %s", newServerName)

	// Configure external cluster for backup source
	if err := p.configureExternalCluster(itemContent, serverName, barmanObjectName); err != nil {
		return nil, errors.Wrap(err, "failed to configure external cluster")
	}
	p.log.Info("Configured externalClusters with backup source")

	// Update bootstrap to use recovery
	if err := p.configureBootstrapRecovery(itemContent); err != nil {
		return nil, errors.Wrap(err, "failed to configure bootstrap recovery")
	}
	p.log.Info("Configured bootstrap.recovery to restore from backup")

	// Update the item with modified content
	input.Item.SetUnstructuredContent(itemContent)
	p.log.Info("Successfully configured cluster for recovery from backup")

	out := velero.NewRestoreItemActionExecuteOutput(input.Item)
	return out, nil
}

func (p *RestorePluginV2) Progress(operationID string, restore *v1.Restore) (velero.OperationProgress, error) {
	progress := velero.OperationProgress{}
	return progress, nil
}

func (p *RestorePluginV2) Cancel(operationID string, restore *v1.Restore) error {
	return nil
}

func (p *RestorePluginV2) AreAdditionalItemsReady(additionalItems []velero.ResourceIdentifier, restore *v1.Restore) (bool, error) {
	return true, nil
}
