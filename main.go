package main

import (
	"github.com/sirupsen/logrus"
	"github.com/vmware-tanzu/velero-plugin-example/internal/plugin"
	"github.com/vmware-tanzu/velero/pkg/plugin/framework"
)

func main() {
	framework.NewServer().
		RegisterRestoreItemActionV2("example.io/restore-pluginv2", newRestorePluginV2).
		RegisterBackupItemActionV2("example.io/backup-pluginv2", newBackupPluginV2).
		Serve()
}

func newBackupPluginV2(logger logrus.FieldLogger) (interface{}, error) {
	return plugin.NewBackupPluginV2(logger), nil
}

func newRestorePluginV2(logger logrus.FieldLogger) (interface{}, error) {
	return plugin.NewRestorePluginV2(logger), nil
}
