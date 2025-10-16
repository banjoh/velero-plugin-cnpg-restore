package main

import (
	"github.com/nvanthao/velero-plugin-cnpg-restore/internal/plugin"
	"github.com/sirupsen/logrus"
	"github.com/vmware-tanzu/velero/pkg/plugin/framework"
)

func main() {
	framework.NewServer().
		RegisterRestoreItemActionV2("replicated.com/cnpg-restore-plugin", newRestorePluginV2).
		RegisterBackupItemActionV2("replicated.com/cnpg-backup-plugin", newBackupPluginV2).
		Serve()
}

func newBackupPluginV2(logger logrus.FieldLogger) (interface{}, error) {
	return plugin.NewBackupPluginV2(logger), nil
}

func newRestorePluginV2(logger logrus.FieldLogger) (interface{}, error) {
	return plugin.NewRestorePluginV2(logger), nil
}
