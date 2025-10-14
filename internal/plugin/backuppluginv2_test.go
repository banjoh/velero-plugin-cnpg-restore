package plugin

import (
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestExtractPluginParameters(t *testing.T) {
	plugin := &BackupPluginV2{
		log: logrus.New(),
	}

	tests := []struct {
		name               string
		itemContent        map[string]interface{}
		expectedServerName string
		expectedError      bool
	}{
		{
			name: "valid serverName in plugins",
			itemContent: map[string]interface{}{
				"apiVersion": "postgresql.cnpg.io/v1",
				"kind":       "Cluster",
				"metadata": map[string]interface{}{
					"name":      "chef-360-cnpg-postgres",
					"namespace": "chef-360",
				},
				"spec": map[string]interface{}{
					"plugins": []interface{}{
						map[string]interface{}{
							"enabled":       true,
							"isWALArchiver": true,
							"name":          "barman-cloud.cloudnative-pg.io",
							"parameters": map[string]interface{}{
								"barmanObjectName": "chef-360-cnpg-backup-store",
								"serverName":       "cnpg-202510131354",
							},
						},
					},
				},
			},
			expectedServerName: "cnpg-202510131354",
			expectedError:      false,
		},
		{
			name: "no plugins in spec",
			itemContent: map[string]interface{}{
				"apiVersion": "postgresql.cnpg.io/v1",
				"kind":       "Cluster",
				"metadata": map[string]interface{}{
					"name":      "test-cluster",
					"namespace": "default",
				},
				"spec": map[string]interface{}{},
			},
			expectedServerName: "",
			expectedError:      false,
		},
		{
			name: "no serverName in parameters",
			itemContent: map[string]interface{}{
				"apiVersion": "postgresql.cnpg.io/v1",
				"kind":       "Cluster",
				"metadata": map[string]interface{}{
					"name":      "test-cluster",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"plugins": []interface{}{
						map[string]interface{}{
							"enabled": true,
							"name":    "barman-cloud.cloudnative-pg.io",
							"parameters": map[string]interface{}{
								"barmanObjectName": "backup-store",
							},
						},
					},
				},
			},
			expectedServerName: "",
			expectedError:      false,
		},
		{
			name: "multiple plugins, serverName in second",
			itemContent: map[string]interface{}{
				"apiVersion": "postgresql.cnpg.io/v1",
				"kind":       "Cluster",
				"metadata": map[string]interface{}{
					"name":      "test-cluster",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"plugins": []interface{}{
						map[string]interface{}{
							"enabled": true,
							"name":    "first-plugin",
							"parameters": map[string]interface{}{
								"someParam": "someValue",
							},
						},
						map[string]interface{}{
							"enabled": true,
							"name":    "barman-cloud.cloudnative-pg.io",
							"parameters": map[string]interface{}{
								"serverName":       "test-server-123",
								"barmanObjectName": "test-backup-store",
							},
						},
					},
				},
			},
			expectedServerName: "test-server-123",
			expectedError:      false,
		},
		{
			name: "no spec field",
			itemContent: map[string]interface{}{
				"apiVersion": "postgresql.cnpg.io/v1",
				"kind":       "Cluster",
				"metadata": map[string]interface{}{
					"name":      "test-cluster",
					"namespace": "default",
				},
			},
			expectedServerName: "",
			expectedError:      true,
		},
		{
			name: "spec is not a map",
			itemContent: map[string]interface{}{
				"apiVersion": "postgresql.cnpg.io/v1",
				"kind":       "Cluster",
				"metadata": map[string]interface{}{
					"name":      "test-cluster",
					"namespace": "default",
				},
				"spec": "invalid-spec",
			},
			expectedServerName: "",
			expectedError:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serverName, err := plugin.extractPluginParameters(tt.itemContent)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedServerName, serverName)
			}
		})
	}
}

func TestAddAnnotation(t *testing.T) {
	plugin := &BackupPluginV2{
		log: logrus.New(),
	}

	tests := []struct {
		name          string
		itemContent   map[string]interface{}
		key           string
		value         string
		expectedError bool
		validateFn    func(t *testing.T, itemContent map[string]interface{})
	}{
		{
			name: "add annotation to existing annotations",
			itemContent: map[string]interface{}{
				"apiVersion": "postgresql.cnpg.io/v1",
				"kind":       "Cluster",
				"metadata": map[string]interface{}{
					"name":      "test-cluster",
					"namespace": "default",
					"annotations": map[string]interface{}{
						"existing-annotation": "existing-value",
					},
				},
			},
			key:           "cnpg.io/serverName",
			value:         "test-server-123",
			expectedError: false,
			validateFn: func(t *testing.T, itemContent map[string]interface{}) {
				metadata := itemContent["metadata"].(map[string]interface{})
				annotations := metadata["annotations"].(map[string]interface{})
				assert.Equal(t, "test-server-123", annotations["cnpg.io/serverName"])
				assert.Equal(t, "existing-value", annotations["existing-annotation"])
			},
		},
		{
			name: "add annotation when no existing annotations",
			itemContent: map[string]interface{}{
				"apiVersion": "postgresql.cnpg.io/v1",
				"kind":       "Cluster",
				"metadata": map[string]interface{}{
					"name":      "test-cluster",
					"namespace": "default",
				},
			},
			key:           "cnpg.io/serverName",
			value:         "test-server-456",
			expectedError: false,
			validateFn: func(t *testing.T, itemContent map[string]interface{}) {
				metadata := itemContent["metadata"].(map[string]interface{})
				annotations := metadata["annotations"].(map[string]interface{})
				assert.Equal(t, "test-server-456", annotations["cnpg.io/serverName"])
			},
		},
		{
			name: "no metadata field",
			itemContent: map[string]interface{}{
				"apiVersion": "postgresql.cnpg.io/v1",
				"kind":       "Cluster",
			},
			key:           "cnpg.io/serverName",
			value:         "test-server",
			expectedError: true,
		},
		{
			name: "metadata is not a map",
			itemContent: map[string]interface{}{
				"apiVersion": "postgresql.cnpg.io/v1",
				"kind":       "Cluster",
				"metadata":   "invalid-metadata",
			},
			key:           "cnpg.io/serverName",
			value:         "test-server",
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := plugin.addAnnotation(tt.itemContent, tt.key, tt.value)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.validateFn != nil {
					tt.validateFn(t, tt.itemContent)
				}
			}
		})
	}
}

func TestBackupExecute(t *testing.T) {
	plugin := &BackupPluginV2{
		log: logrus.New(),
	}

	tests := []struct {
		name                         string
		itemContent                  map[string]interface{}
		expectedError                bool
		expectedServerNameAnnotation string
	}{
		{
			name: "successful execution with serverName parameter",
			itemContent: map[string]interface{}{
				"apiVersion": "postgresql.cnpg.io/v1",
				"kind":       "Cluster",
				"metadata": map[string]interface{}{
					"name":      "chef-360-cnpg-postgres",
					"namespace": "chef-360",
					"annotations": map[string]interface{}{
						"meta.helm.sh/release-name":      "chef-360",
						"meta.helm.sh/release-namespace": "chef-360",
					},
				},
				"spec": map[string]interface{}{
					"instances": 1,
					"plugins": []interface{}{
						map[string]interface{}{
							"enabled":       true,
							"isWALArchiver": true,
							"name":          "barman-cloud.cloudnative-pg.io",
							"parameters": map[string]interface{}{
								"barmanObjectName": "chef-360-cnpg-backup-store",
								"serverName":       "cnpg-202510131354",
							},
						},
					},
				},
			},
			expectedError:                false,
			expectedServerNameAnnotation: "cnpg-202510131354",
		},
		{
			name: "no parameters - should skip annotation",
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
			expectedError:                false,
			expectedServerNameAnnotation: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create unstructured item
			item := &unstructured.Unstructured{}
			item.SetUnstructuredContent(tt.itemContent)

			resultItem, additionalItems, operationID, itemsToUpdate, err := plugin.Execute(item, nil)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Nil(t, additionalItems)
				assert.Equal(t, "", operationID)
				assert.Nil(t, itemsToUpdate)

				// Check if annotations were added correctly
				resultContent := resultItem.UnstructuredContent()
				metadata := resultContent["metadata"].(map[string]interface{})

				if tt.expectedServerNameAnnotation != "" {
					annotations := metadata["annotations"].(map[string]interface{})
					assert.Equal(t, tt.expectedServerNameAnnotation, annotations["velero-cnpg/serverName"])
				} else {
					// If no annotation expected, check that it doesn't exist
					if annotations, exists := metadata["annotations"]; exists {
						annotationsMap := annotations.(map[string]interface{})
						_, hasServerName := annotationsMap["velero-cnpg/serverName"]
						assert.False(t, hasServerName)
					}
				}
			}
		})
	}
}
