package plugin

import (
	"github.com/sirupsen/logrus"
	v1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	"github.com/vmware-tanzu/velero/pkg/plugin/velero"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	// MigrationInitContainerName is the name of the init container that waits for migration jobs
	MigrationInitContainerName = "wait-for-migration-job"
)

// DeploymentRestorePlugin is a restore item action plugin for Velero that handles deployments
type DeploymentRestorePlugin struct {
	log logrus.FieldLogger
}

// NewDeploymentRestorePlugin instantiates a new DeploymentRestorePlugin.
func NewDeploymentRestorePlugin(log logrus.FieldLogger) *DeploymentRestorePlugin {
	return &DeploymentRestorePlugin{log: log}
}

// Name is required to implement the interface, but the Velero pod does not delegate this
// method -- it's used to tell velero what name it was registered under. The plugin implementation
// must define it, but it will never actually be called.
func (p *DeploymentRestorePlugin) Name() string {
	return "deploymentRestorePlugin"
}

// AppliesTo returns information about which resources this action should be invoked for.
// The IncludedResources and ExcludedResources slices can include both resources
// and resources with group names. These work: "ingresses", "ingresses.extensions".
// A RestoreItemAction's Execute function will only be invoked on items that match the returned
// selector. A zero-valued ResourceSelector matches all resources.
func (p *DeploymentRestorePlugin) AppliesTo() (velero.ResourceSelector, error) {
	p.log.Info("DeploymentRestorePlugin.AppliesTo called")
	return velero.ResourceSelector{
		IncludedResources: []string{"deployments"},
	}, nil
}

// Execute allows the DeploymentRestorePlugin to perform arbitrary logic with the item being restored,
// in this case, removing init containers named "wait-for-migration-job".
func (p *DeploymentRestorePlugin) Execute(input *velero.RestoreItemActionExecuteInput) (*velero.RestoreItemActionExecuteOutput, error) {
	p.log.Info("Executing deployment restore plugin on resource: %s", resourceName(input.Item))

	itemContent := input.Item.UnstructuredContent()

	// Check if this deployment has init containers
	initContainers, found, err := unstructured.NestedFieldNoCopy(itemContent, "spec", "template", "spec", "initContainers")
	if err != nil {
		p.log.Warnf("Failed to get initContainers field: %v", err)
		// Return unchanged deployment on error
		out := velero.NewRestoreItemActionExecuteOutput(input.Item)
		return out, nil
	}

	if !found {
		p.log.Info("No initContainers found in deployment, skipping")
		out := velero.NewRestoreItemActionExecuteOutput(input.Item)
		return out, nil
	}

	initContainersList, ok := initContainers.([]interface{})
	if !ok {
		p.log.Warn("initContainers is not a list, skipping")
		out := velero.NewRestoreItemActionExecuteOutput(input.Item)
		return out, nil
	}

	// Filter out init containers named "wait-for-migration-job"
	var filteredContainers []interface{}
	removedCount := 0

	for _, container := range initContainersList {
		containerMap, ok := container.(map[string]interface{})
		if !ok {
			// Keep malformed containers as-is
			filteredContainers = append(filteredContainers, container)
			continue
		}

		name, found := containerMap["name"]
		if !found {
			// Keep containers without name as-is
			filteredContainers = append(filteredContainers, container)
			continue
		}

		nameStr, ok := name.(string)
		if !ok {
			// Keep containers with non-string name as-is
			filteredContainers = append(filteredContainers, container)
			continue
		}

		if nameStr == MigrationInitContainerName {
			p.log.Infof("Removing init container: %s", nameStr)
			removedCount++
		} else {
			filteredContainers = append(filteredContainers, container)
		}
	}

	if removedCount > 0 {
		p.log.Infof("Removed %d '%s' init container(s)", removedCount, MigrationInitContainerName)

		// Update the deployment with filtered init containers
		if len(filteredContainers) == 0 {
			// Remove the initContainers field entirely if no containers remain
			unstructured.RemoveNestedField(itemContent, "spec", "template", "spec", "initContainers")
		} else {
			// Set the filtered containers
			err = unstructured.SetNestedField(itemContent, filteredContainers, "spec", "template", "spec", "initContainers")
			if err != nil {
				p.log.Warnf("Failed to update initContainers: %v", err)
				// Return unchanged deployment on error
				out := velero.NewRestoreItemActionExecuteOutput(input.Item)
				return out, nil
			}
		}

		// Update the item with modified content
		input.Item.SetUnstructuredContent(itemContent)
		p.log.Info("Successfully removed migration init containers from deployment")
	} else {
		p.log.Infof("No '%s' init containers found, deployment unchanged", MigrationInitContainerName)
	}

	out := velero.NewRestoreItemActionExecuteOutput(input.Item)
	return out, nil
}

func (p *DeploymentRestorePlugin) Progress(operationID string, restore *v1.Restore) (velero.OperationProgress, error) {
	progress := velero.OperationProgress{}
	return progress, nil
}

func (p *DeploymentRestorePlugin) Cancel(operationID string, restore *v1.Restore) error {
	return nil
}

func (p *DeploymentRestorePlugin) AreAdditionalItemsReady(additionalItems []velero.ResourceIdentifier, restore *v1.Restore) (bool, error) {
	return true, nil
}
