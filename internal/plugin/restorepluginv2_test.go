package plugin

import (
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"github.com/vmware-tanzu/velero/pkg/plugin/velero"
)

func TestGetAnnotation(t *testing.T) {
	plugin := &RestorePluginV2{
		log: logrus.New(),
	}

	tests := []struct {
		name          string
		itemContent   map[string]interface{}
		key           string
		expectedValue string
		expectedFound bool
		expectedError bool
	}{
		{
			name: "annotation exists",
			itemContent: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name":      "test-cluster",
					"namespace": "default",
					"annotations": map[string]interface{}{
						"cnpg.io/serverName": "test-server-123",
					},
				},
			},
			key:           "cnpg.io/serverName",
			expectedValue: "test-server-123",
			expectedFound: true,
			expectedError: false,
		},
		{
			name: "annotation does not exist",
			itemContent: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name":      "test-cluster",
					"namespace": "default",
					"annotations": map[string]interface{}{
						"other-annotation": "value",
					},
				},
			},
			key:           "cnpg.io/serverName",
			expectedValue: "",
			expectedFound: false,
			expectedError: false,
		},
		{
			name: "no annotations",
			itemContent: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name":      "test-cluster",
					"namespace": "default",
				},
			},
			key:           "cnpg.io/serverName",
			expectedValue: "",
			expectedFound: false,
			expectedError: false,
		},
		{
			name: "no metadata",
			itemContent: map[string]interface{}{
				"spec": map[string]interface{}{},
			},
			key:           "cnpg.io/serverName",
			expectedValue: "",
			expectedFound: false,
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, found, err := plugin.getAnnotation(tt.itemContent, tt.key)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedFound, found)
				assert.Equal(t, tt.expectedValue, value)
			}
		})
	}
}

func TestRemoveEphemeralFields(t *testing.T) {
	plugin := &RestorePluginV2{
		log: logrus.New(),
	}

	itemContent := map[string]interface{}{
		"apiVersion": "postgresql.cnpg.io/v1",
		"kind":       "Cluster",
		"metadata": map[string]interface{}{
			"name":              "test-cluster",
			"namespace":         "default",
			"resourceVersion":   "12345",
			"uid":               "abc-123",
			"generation":        2,
			"creationTimestamp": "2025-10-13T00:00:00Z",
			"managedFields":     []interface{}{},
			"annotations": map[string]interface{}{
				"cnpg.io/serverName": "test-server",
			},
		},
		"spec": map[string]interface{}{
			"instances": 1,
		},
		"status": map[string]interface{}{
			"phase": "Running",
		},
	}

	plugin.removeEphemeralFields(itemContent)

	// Check that status is removed
	_, hasStatus := itemContent["status"]
	assert.False(t, hasStatus, "status field should be removed")

	// Check that spec and apiVersion are preserved
	assert.NotNil(t, itemContent["spec"])
	assert.NotNil(t, itemContent["apiVersion"])

	// Check that ephemeral metadata fields are removed
	metadata := itemContent["metadata"].(map[string]interface{})
	_, hasResourceVersion := metadata["resourceVersion"]
	assert.False(t, hasResourceVersion, "resourceVersion should be removed")

	_, hasUID := metadata["uid"]
	assert.False(t, hasUID, "uid should be removed")

	_, hasGeneration := metadata["generation"]
	assert.False(t, hasGeneration, "generation should be removed")

	_, hasCreationTimestamp := metadata["creationTimestamp"]
	assert.False(t, hasCreationTimestamp, "creationTimestamp should be removed")

	_, hasManagedFields := metadata["managedFields"]
	assert.False(t, hasManagedFields, "managedFields should be removed")

	// Check that important metadata is preserved
	assert.Equal(t, "test-cluster", metadata["name"])
	assert.Equal(t, "default", metadata["namespace"])
	assert.NotNil(t, metadata["annotations"])
}

func TestConfigureExternalCluster(t *testing.T) {
	plugin := &RestorePluginV2{
		log: logrus.New(),
	}

	tests := []struct {
		name              string
		itemContent       map[string]interface{}
		serverName        string
		barmanObjectName  string
		expectedError     bool
	}{
		{
			name: "successful configuration",
			itemContent: map[string]interface{}{
				"spec": map[string]interface{}{
					"instances": 1,
				},
			},
			serverName:       "test-server-123",
			barmanObjectName: "backup-store",
			expectedError:    false,
		},
		{
			name:              "no spec field",
			itemContent:       map[string]interface{}{},
			serverName:        "test-server",
			barmanObjectName:  "backup-store",
			expectedError:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := plugin.configureExternalCluster(tt.itemContent, tt.serverName, tt.barmanObjectName)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)

				spec := tt.itemContent["spec"].(map[string]interface{})
				externalClusters := spec["externalClusters"].([]interface{})

				assert.Len(t, externalClusters, 1)

				cluster := externalClusters[0].(map[string]interface{})
				assert.Equal(t, "clusterBackup", cluster["name"])

				pluginConfig := cluster["plugin"].(map[string]interface{})
				assert.Equal(t, "barman-cloud.cloudnative-pg.io", pluginConfig["name"])

				params := pluginConfig["parameters"].(map[string]interface{})
				assert.Equal(t, tt.barmanObjectName, params["barmanObjectName"])
				assert.Equal(t, tt.serverName, params["serverName"])
			}
		})
	}
}

func TestGenerateNewServerName(t *testing.T) {
	plugin := &RestorePluginV2{
		log: logrus.New(),
	}

	tests := []struct {
		name        string
		clusterName string
	}{
		{
			name:        "simple cluster name",
			clusterName: "my-cluster",
		},
		{
			name:        "cluster name with dashes",
			clusterName: "chef-360-cnpg-postgres",
		},
		{
			name:        "short cluster name",
			clusterName: "db",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serverName := plugin.generateNewServerName(tt.clusterName)

			// Check that it starts with the cluster name
			assert.Contains(t, serverName, tt.clusterName)

			// Check that it has a timestamp suffix (format: clusterName-YYYYMMDD-HHMMSS)
			assert.Regexp(t, `^`+tt.clusterName+`-\d{8}-\d{6}$`, serverName)

			// Generate another one and ensure they're different (different timestamps)
			// Note: In rare cases this could fail if both are generated in the same second
		})
	}
}

func TestUpdatePluginServerName(t *testing.T) {
	plugin := &RestorePluginV2{
		log: logrus.New(),
	}

	tests := []struct {
		name          string
		itemContent   map[string]interface{}
		newServerName string
		expectedError bool
		validateFn    func(t *testing.T, itemContent map[string]interface{})
	}{
		{
			name: "successful update with single plugin",
			itemContent: map[string]interface{}{
				"spec": map[string]interface{}{
					"instances": 1,
					"plugins": []interface{}{
						map[string]interface{}{
							"name": "barman-cloud.cloudnative-pg.io",
							"parameters": map[string]interface{}{
								"serverName":       "old-server-name",
								"barmanObjectName": "backup-store",
							},
						},
					},
				},
			},
			newServerName: "new-cluster-20250114-143025",
			expectedError: false,
			validateFn: func(t *testing.T, itemContent map[string]interface{}) {
				spec := itemContent["spec"].(map[string]interface{})
				plugins := spec["plugins"].([]interface{})
				plugin := plugins[0].(map[string]interface{})
				params := plugin["parameters"].(map[string]interface{})
				assert.Equal(t, "new-cluster-20250114-143025", params["serverName"])
				assert.Equal(t, "backup-store", params["barmanObjectName"])
			},
		},
		{
			name: "successful update with multiple plugins",
			itemContent: map[string]interface{}{
				"spec": map[string]interface{}{
					"instances": 1,
					"plugins": []interface{}{
						map[string]interface{}{
							"name": "barman-cloud.cloudnative-pg.io",
							"parameters": map[string]interface{}{
								"serverName":       "old-server-1",
								"barmanObjectName": "backup-store-1",
							},
						},
						map[string]interface{}{
							"name": "another-plugin",
							"parameters": map[string]interface{}{
								"serverName": "old-server-2",
								"otherParam": "value",
							},
						},
					},
				},
			},
			newServerName: "new-cluster-20250114-143025",
			expectedError: false,
			validateFn: func(t *testing.T, itemContent map[string]interface{}) {
				spec := itemContent["spec"].(map[string]interface{})
				plugins := spec["plugins"].([]interface{})

				// Check first plugin
				plugin1 := plugins[0].(map[string]interface{})
				params1 := plugin1["parameters"].(map[string]interface{})
				assert.Equal(t, "new-cluster-20250114-143025", params1["serverName"])
				assert.Equal(t, "backup-store-1", params1["barmanObjectName"])

				// Check second plugin
				plugin2 := plugins[1].(map[string]interface{})
				params2 := plugin2["parameters"].(map[string]interface{})
				assert.Equal(t, "new-cluster-20250114-143025", params2["serverName"])
				assert.Equal(t, "value", params2["otherParam"])
			},
		},
		{
			name: "plugin without serverName parameter",
			itemContent: map[string]interface{}{
				"spec": map[string]interface{}{
					"instances": 1,
					"plugins": []interface{}{
						map[string]interface{}{
							"name": "some-plugin",
							"parameters": map[string]interface{}{
								"otherParam": "value",
							},
						},
					},
				},
			},
			newServerName: "new-cluster-20250114-143025",
			expectedError: false,
			validateFn: func(t *testing.T, itemContent map[string]interface{}) {
				spec := itemContent["spec"].(map[string]interface{})
				plugins := spec["plugins"].([]interface{})
				plugin := plugins[0].(map[string]interface{})
				params := plugin["parameters"].(map[string]interface{})

				// serverName should not be added if it didn't exist
				_, hasServerName := params["serverName"]
				assert.False(t, hasServerName)
				assert.Equal(t, "value", params["otherParam"])
			},
		},
		{
			name: "no plugins in spec",
			itemContent: map[string]interface{}{
				"spec": map[string]interface{}{
					"instances": 1,
				},
			},
			newServerName: "new-cluster-20250114-143025",
			expectedError: false,
			validateFn: func(t *testing.T, itemContent map[string]interface{}) {
				spec := itemContent["spec"].(map[string]interface{})
				_, hasPlugins := spec["plugins"]
				assert.False(t, hasPlugins)
			},
		},
		{
			name: "empty plugins array",
			itemContent: map[string]interface{}{
				"spec": map[string]interface{}{
					"instances": 1,
					"plugins":   []interface{}{},
				},
			},
			newServerName: "new-cluster-20250114-143025",
			expectedError: false,
			validateFn: func(t *testing.T, itemContent map[string]interface{}) {
				spec := itemContent["spec"].(map[string]interface{})
				plugins := spec["plugins"].([]interface{})
				assert.Len(t, plugins, 0)
			},
		},
		{
			name:          "no spec field",
			itemContent:   map[string]interface{}{},
			newServerName: "new-cluster-20250114-143025",
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := plugin.updatePluginServerName(tt.itemContent, tt.newServerName)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)

				if tt.validateFn != nil {
					tt.validateFn(t, tt.itemContent)
				}
			}
		})
	}
}

func TestConfigureBootstrapRecovery(t *testing.T) {
	plugin := &RestorePluginV2{
		log: logrus.New(),
	}

	tests := []struct {
		name          string
		itemContent   map[string]interface{}
		expectedError bool
	}{
		{
			name: "successful configuration - replace initdb",
			itemContent: map[string]interface{}{
				"spec": map[string]interface{}{
					"instances": 1,
					"bootstrap": map[string]interface{}{
						"initdb": map[string]interface{}{
							"database": "app",
						},
					},
				},
			},
			expectedError: false,
		},
		{
			name: "successful configuration - no existing bootstrap",
			itemContent: map[string]interface{}{
				"spec": map[string]interface{}{
					"instances": 1,
				},
			},
			expectedError: false,
		},
		{
			name:          "no spec field",
			itemContent:   map[string]interface{}{},
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := plugin.configureBootstrapRecovery(tt.itemContent)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)

				spec := tt.itemContent["spec"].(map[string]interface{})
				bootstrap := spec["bootstrap"].(map[string]interface{})

				// Check that initdb is replaced with recovery
				_, hasInitDB := bootstrap["initdb"]
				assert.False(t, hasInitDB, "initdb should be replaced")

				recovery := bootstrap["recovery"].(map[string]interface{})
				assert.Equal(t, "clusterBackup", recovery["source"])
			}
		})
	}
}

func TestRestoreExecute(t *testing.T) {
	plugin := &RestorePluginV2{
		log: logrus.New(),
	}

	tests := []struct {
		name          string
		itemContent   map[string]interface{}
		expectedError bool
		validateFn    func(t *testing.T, output *velero.RestoreItemActionExecuteOutput)
	}{
		{
			name: "successful restore with annotations and plugin serverName update",
			itemContent: map[string]interface{}{
				"apiVersion": "postgresql.cnpg.io/v1",
				"kind":       "Cluster",
				"metadata": map[string]interface{}{
					"name":              "chef-360-cnpg-postgres",
					"namespace":         "chef-360",
					"resourceVersion":   "50194",
					"uid":               "4bbf7651-2056-4cf7-b5cb-ff7ce4b806cf",
					"generation":        2,
					"creationTimestamp": "2025-10-13T02:58:32Z",
					"annotations": map[string]interface{}{
						"cnpg.io/serverName":       "cnpg-202510131354",
						"cnpg.io/barmanObjectName": "chef-360-cnpg-backup-store",
					},
				},
				"spec": map[string]interface{}{
					"instances": 1,
					"bootstrap": map[string]interface{}{
						"initdb": map[string]interface{}{
							"database": "app",
						},
					},
					"plugins": []interface{}{
						map[string]interface{}{
							"name": "barman-cloud.cloudnative-pg.io",
							"parameters": map[string]interface{}{
								"serverName":       "cnpg-202510131354",
								"barmanObjectName": "chef-360-cnpg-backup-store",
							},
						},
					},
				},
				"status": map[string]interface{}{
					"phase": "Running",
				},
			},
			expectedError: false,
			validateFn: func(t *testing.T, output *velero.RestoreItemActionExecuteOutput) {
				itemContent := output.UpdatedItem.UnstructuredContent()

				// Check status is removed
				_, hasStatus := itemContent["status"]
				assert.False(t, hasStatus)

				// Check ephemeral metadata fields are removed
				metadata := itemContent["metadata"].(map[string]interface{})
				_, hasResourceVersion := metadata["resourceVersion"]
				assert.False(t, hasResourceVersion)

				spec := itemContent["spec"].(map[string]interface{})

				// Check plugin serverName was updated to new unique value
				plugins := spec["plugins"].([]interface{})
				assert.Len(t, plugins, 1)
				pluginConfig := plugins[0].(map[string]interface{})
				params := pluginConfig["parameters"].(map[string]interface{})

				// Verify new serverName follows format: clusterName-YYYYMMDD-HHMMSS
				newServerName := params["serverName"].(string)
				assert.NotEqual(t, "cnpg-202510131354", newServerName, "serverName should be updated")
				assert.Regexp(t, `^chef-360-cnpg-postgres-\d{8}-\d{6}$`, newServerName)

				// Verify barmanObjectName is preserved
				assert.Equal(t, "chef-360-cnpg-backup-store", params["barmanObjectName"])

				// Check externalClusters is configured with ORIGINAL serverName (for backup source)
				externalClusters := spec["externalClusters"].([]interface{})
				assert.Len(t, externalClusters, 1)

				cluster := externalClusters[0].(map[string]interface{})
				assert.Equal(t, "clusterBackup", cluster["name"])

				extPlugin := cluster["plugin"].(map[string]interface{})
				extParams := extPlugin["parameters"].(map[string]interface{})
				assert.Equal(t, "cnpg-202510131354", extParams["serverName"], "externalClusters should use original serverName")
				assert.Equal(t, "chef-360-cnpg-backup-store", extParams["barmanObjectName"])

				// Check bootstrap is updated to recovery
				bootstrap := spec["bootstrap"].(map[string]interface{})
				_, hasInitDB := bootstrap["initdb"]
				assert.False(t, hasInitDB)

				recovery := bootstrap["recovery"].(map[string]interface{})
				assert.Equal(t, "clusterBackup", recovery["source"])
			},
		},
		{
			name: "successful restore with annotations",
			itemContent: map[string]interface{}{
				"apiVersion": "postgresql.cnpg.io/v1",
				"kind":       "Cluster",
				"metadata": map[string]interface{}{
					"name":              "chef-360-cnpg-postgres",
					"namespace":         "chef-360",
					"resourceVersion":   "50194",
					"uid":               "4bbf7651-2056-4cf7-b5cb-ff7ce4b806cf",
					"generation":        2,
					"creationTimestamp": "2025-10-13T02:58:32Z",
					"annotations": map[string]interface{}{
						"cnpg.io/serverName":       "cnpg-202510131354",
						"cnpg.io/barmanObjectName": "chef-360-cnpg-backup-store",
					},
				},
				"spec": map[string]interface{}{
					"instances": 1,
					"bootstrap": map[string]interface{}{
						"initdb": map[string]interface{}{
							"database": "app",
						},
					},
				},
				"status": map[string]interface{}{
					"phase": "Running",
				},
			},
			expectedError: false,
			validateFn: func(t *testing.T, output *velero.RestoreItemActionExecuteOutput) {
				itemContent := output.UpdatedItem.UnstructuredContent()

				// Check status is removed
				_, hasStatus := itemContent["status"]
				assert.False(t, hasStatus)

				// Check ephemeral metadata fields are removed
				metadata := itemContent["metadata"].(map[string]interface{})
				_, hasResourceVersion := metadata["resourceVersion"]
				assert.False(t, hasResourceVersion)

				// Check externalClusters is configured
				spec := itemContent["spec"].(map[string]interface{})
				externalClusters := spec["externalClusters"].([]interface{})
				assert.Len(t, externalClusters, 1)

				cluster := externalClusters[0].(map[string]interface{})
				assert.Equal(t, "clusterBackup", cluster["name"])

				// Check bootstrap is updated to recovery
				bootstrap := spec["bootstrap"].(map[string]interface{})
				_, hasInitDB := bootstrap["initdb"]
				assert.False(t, hasInitDB)

				recovery := bootstrap["recovery"].(map[string]interface{})
				assert.Equal(t, "clusterBackup", recovery["source"])
			},
		},
		{
			name: "no annotations - skip modifications",
			itemContent: map[string]interface{}{
				"apiVersion": "postgresql.cnpg.io/v1",
				"kind":       "Cluster",
				"metadata": map[string]interface{}{
					"name":      "test-cluster",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"instances": 1,
				},
			},
			expectedError: false,
			validateFn: func(t *testing.T, output *velero.RestoreItemActionExecuteOutput) {
				itemContent := output.UpdatedItem.UnstructuredContent()

				// Check that spec is unchanged
				spec := itemContent["spec"].(map[string]interface{})
				_, hasExternalClusters := spec["externalClusters"]
				assert.False(t, hasExternalClusters)
			},
		},
		{
			name: "serverName annotation without barmanObjectName - error",
			itemContent: map[string]interface{}{
				"apiVersion": "postgresql.cnpg.io/v1",
				"kind":       "Cluster",
				"metadata": map[string]interface{}{
					"name":      "test-cluster",
					"namespace": "default",
					"annotations": map[string]interface{}{
						"cnpg.io/serverName": "test-server",
					},
				},
				"spec": map[string]interface{}{
					"instances": 1,
				},
			},
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := &unstructured.Unstructured{}
			item.SetUnstructuredContent(tt.itemContent)

			input := &velero.RestoreItemActionExecuteInput{
				Item: item,
			}

			output, err := plugin.Execute(input)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, output)

				if tt.validateFn != nil {
					tt.validateFn(t, output)
				}
			}
		})
	}
}
