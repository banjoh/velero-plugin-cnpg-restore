package plugin

import (
	"github.com/sirupsen/logrus"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/pkg/errors"
	v1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	"github.com/vmware-tanzu/velero/pkg/plugin/velero"
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
	p.log.Info("BackupPluginV2.AppliesTo called")
	return velero.ResourceSelector{
		IncludedResources: []string{"clusters.postgresql.cnpg.io"},
	}, nil
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

// Execute allows the ItemAction to perform arbitrary logic with the item being backed up
func (p *BackupPluginV2) Execute(item runtime.Unstructured, backup *v1.Backup) (runtime.Unstructured, []velero.ResourceIdentifier, string, []velero.ResourceIdentifier, error) {
	p.log.Info("BackupPluginV2.Execute - TODO: implement backup logic")
	// TODO: annotate the Cluster CR with the serverName in .spec.plugins.parameters.serverName
	// https://cloudnative-pg.io/documentation/1.27/cloudnative-pg.v1/#postgresql-cnpg-io-v1-ClusterSpec
	// this is so that the restored cluster can get current serverName and derive a new serverName to avoid WAL conflicts
	return item, nil, "", nil, nil
}

func (p *BackupPluginV2) Progress(operationID string, backup *v1.Backup) (velero.OperationProgress, error) {
	progress := velero.OperationProgress{}
	return progress, nil
}

func (p *BackupPluginV2) Cancel(operationID string, backup *v1.Backup) error {
	return nil
}
