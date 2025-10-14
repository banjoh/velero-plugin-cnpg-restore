package plugin

import (
	"github.com/sirupsen/logrus"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/pkg/errors"
	v1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	"github.com/vmware-tanzu/velero/pkg/plugin/velero"
)

const (
	// AnnotationServerName is the annotation key used to store the CNPG server name
	// for restore operations to reference the backup source
	AnnotationServerName = "velero-cnpg/serverName"
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
