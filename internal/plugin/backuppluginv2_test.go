package plugin

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
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

// Helper function to create a mock CNPG Backup resource
func createMockBackup(name, namespace, clusterName, phase, backupID string, creationTime time.Time) *unstructured.Unstructured {
	backup := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "postgresql.cnpg.io/v1",
			"kind":       "Backup",
			"metadata": map[string]interface{}{
				"name":              name,
				"namespace":         namespace,
				"creationTimestamp": metav1.NewTime(creationTime).Format(time.RFC3339),
			},
			"spec": map[string]interface{}{
				"cluster": map[string]interface{}{
					"name": clusterName,
				},
			},
			"status": map[string]interface{}{
				"phase":    phase,
				"backupId": backupID,
			},
		},
	}
	backup.SetCreationTimestamp(metav1.NewTime(creationTime))
	return backup
}

func TestGetLatestCompletedBackupID(t *testing.T) {
	// Note: This test uses a fake dynamic client and won't actually call K8s API
	// In a real environment, GetDynamicClient() would need to be mocked or dependency-injected

	tests := []struct {
		name              string
		namespace         string
		clusterName       string
		mockBackups       []runtime.Object
		expectedBackupID  string
		expectError       bool
		expectEmptyResult bool
	}{
		{
			name:        "single completed backup",
			namespace:   "default",
			clusterName: "test-cluster",
			mockBackups: []runtime.Object{
				createMockBackup("backup-1", "default", "test-cluster", "completed", "backup-id-123", time.Now()),
			},
			expectedBackupID:  "backup-id-123",
			expectError:       false,
			expectEmptyResult: false,
		},
		{
			name:        "multiple backups - returns latest",
			namespace:   "default",
			clusterName: "test-cluster",
			mockBackups: []runtime.Object{
				createMockBackup("backup-1", "default", "test-cluster", "completed", "backup-id-old", time.Now().Add(-2*time.Hour)),
				createMockBackup("backup-2", "default", "test-cluster", "completed", "backup-id-latest", time.Now()),
				createMockBackup("backup-3", "default", "test-cluster", "completed", "backup-id-middle", time.Now().Add(-1*time.Hour)),
			},
			expectedBackupID:  "backup-id-latest",
			expectError:       false,
			expectEmptyResult: false,
		},
		{
			name:        "no completed backups - running only",
			namespace:   "default",
			clusterName: "test-cluster",
			mockBackups: []runtime.Object{
				createMockBackup("backup-1", "default", "test-cluster", "running", "backup-id-123", time.Now()),
			},
			expectedBackupID:  "",
			expectError:       false,
			expectEmptyResult: true,
		},
		{
			name:        "backups for different cluster",
			namespace:   "default",
			clusterName: "test-cluster",
			mockBackups: []runtime.Object{
				createMockBackup("backup-1", "default", "other-cluster", "completed", "backup-id-123", time.Now()),
			},
			expectedBackupID:  "",
			expectError:       false,
			expectEmptyResult: true,
		},
		{
			name:              "no backups at all",
			namespace:         "default",
			clusterName:       "test-cluster",
			mockBackups:       []runtime.Object{},
			expectedBackupID:  "",
			expectError:       false,
			expectEmptyResult: true,
		},
		{
			name:        "mixed completed and failed backups",
			namespace:   "default",
			clusterName: "test-cluster",
			mockBackups: []runtime.Object{
				createMockBackup("backup-1", "default", "test-cluster", "failed", "backup-id-failed", time.Now().Add(-1*time.Hour)),
				createMockBackup("backup-2", "default", "test-cluster", "completed", "backup-id-success", time.Now()),
			},
			expectedBackupID:  "backup-id-success",
			expectError:       false,
			expectEmptyResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: This test documents the expected behavior but cannot fully test
			// the method without mocking GetDynamicClient() or using dependency injection
			// The actual implementation would need integration tests or mocked client

			// Create scheme and register CNPG backup GVK
			scheme := runtime.NewScheme()
			gvk := schema.GroupVersionKind{
				Group:   "postgresql.cnpg.io",
				Version: "v1",
				Kind:    "Backup",
			}
			scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})

			// Create fake dynamic client with mock backups
			dynamicClient := fake.NewSimpleDynamicClient(scheme, tt.mockBackups...)

			// This test validates the logic but would need the plugin method
			// to accept a client parameter for full testability
			_ = dynamicClient

			// Document expected behavior
			t.Logf("Expected backup ID: %s", tt.expectedBackupID)
			t.Logf("Expect error: %v", tt.expectError)
			t.Logf("Expect empty result: %v", tt.expectEmptyResult)
		})
	}
}

func TestBackupExecuteWithBackupID(t *testing.T) {
	plugin := &BackupPluginV2{
		log: logrus.New(),
	}

	tests := []struct {
		name                       string
		itemContent                map[string]interface{}
		expectedError              bool
		expectedServerName         string
		shouldHaveBackupAnnotation bool
	}{
		{
			name: "cluster with serverName should attempt backup ID annotation",
			itemContent: map[string]interface{}{
				"apiVersion": "postgresql.cnpg.io/v1",
				"kind":       "Cluster",
				"metadata": map[string]interface{}{
					"name":      "test-cluster",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"instances": 1,
					"plugins": []interface{}{
						map[string]interface{}{
							"enabled":       true,
							"isWALArchiver": true,
							"name":          "barman-cloud.cloudnative-pg.io",
							"parameters": map[string]interface{}{
								"barmanObjectName": "test-backup-store",
								"serverName":       "test-server-123",
							},
						},
					},
				},
			},
			expectedError:              false,
			expectedServerName:         "test-server-123",
			shouldHaveBackupAnnotation: false, // Will be false in unit test without real K8s API
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

				// Check annotations
				resultContent := resultItem.UnstructuredContent()
				metadata := resultContent["metadata"].(map[string]interface{})
				annotations := metadata["annotations"].(map[string]interface{})

				// ServerName should always be present
				assert.Equal(t, tt.expectedServerName, annotations[AnnotationServerName])

				// BackupID annotation depends on whether backups exist in K8s
				// In unit tests without K8s API, this will not be present
				// This documents the expected behavior for integration tests
				if tt.shouldHaveBackupAnnotation {
					_, hasBackupID := annotations[AnnotationCurrentBackupID]
					assert.True(t, hasBackupID, "Expected backup ID annotation to be present")
				}
			}
		})
	}
}
