package plugin

import (
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmware-tanzu/velero/pkg/plugin/velero"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestDeploymentRestorePluginAppliesTo(t *testing.T) {
	plugin := &DeploymentRestorePlugin{
		log: logrus.New(),
	}

	selector, err := plugin.AppliesTo()
	require.NoError(t, err)
	assert.Equal(t, []string{"deployments"}, selector.IncludedResources)
	assert.Empty(t, selector.ExcludedResources)
}

func TestDeploymentRestorePluginExecute(t *testing.T) {
	plugin := &DeploymentRestorePlugin{
		log: logrus.New(),
	}

	tests := []struct {
		name          string
		itemContent   map[string]interface{}
		expectedError bool
		validateFn    func(t *testing.T, output *velero.RestoreItemActionExecuteOutput)
	}{
		{
			name: "deployment with wait-for-migration-job init container - should remove it",
			itemContent: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata": map[string]interface{}{
					"name":      "test-deployment",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"replicas": 1,
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"containers": []interface{}{
								map[string]interface{}{
									"name":  "app",
									"image": "nginx:latest",
								},
							},
							"initContainers": []interface{}{
								map[string]interface{}{
									"name":  MigrationInitContainerName,
									"image": "busybox:latest",
									"command": []interface{}{
										"sh",
										"-c",
										"kubectl wait --for=condition=complete job/migration-job",
									},
								},
								map[string]interface{}{
									"name":  "other-init",
									"image": "busybox:latest",
									"command": []interface{}{
										"sh",
										"-c",
										"echo 'other init task'",
									},
								},
							},
						},
					},
				},
			},
			expectedError: false,
			validateFn: func(t *testing.T, output *velero.RestoreItemActionExecuteOutput) {
				itemContent := output.UpdatedItem.UnstructuredContent()
				spec := itemContent["spec"].(map[string]interface{})
				template := spec["template"].(map[string]interface{})
				templateSpec := template["spec"].(map[string]interface{})
				initContainers := templateSpec["initContainers"].([]interface{})

				// Should have only one init container remaining (other-init)
				assert.Len(t, initContainers, 1)

				remainingContainer := initContainers[0].(map[string]interface{})
				assert.Equal(t, "other-init", remainingContainer["name"])
			},
		},
		{
			name: "deployment with multiple wait-for-migration-job init containers - should remove all",
			itemContent: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata": map[string]interface{}{
					"name":      "test-deployment",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"replicas": 1,
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"containers": []interface{}{
								map[string]interface{}{
									"name":  "app",
									"image": "nginx:latest",
								},
							},
							"initContainers": []interface{}{
								map[string]interface{}{
									"name":  MigrationInitContainerName,
									"image": "busybox:latest",
								},
								map[string]interface{}{
									"name":  MigrationInitContainerName,
									"image": "busybox:latest",
								},
								map[string]interface{}{
									"name":  "other-init",
									"image": "busybox:latest",
								},
							},
						},
					},
				},
			},
			expectedError: false,
			validateFn: func(t *testing.T, output *velero.RestoreItemActionExecuteOutput) {
				itemContent := output.UpdatedItem.UnstructuredContent()
				spec := itemContent["spec"].(map[string]interface{})
				template := spec["template"].(map[string]interface{})
				templateSpec := template["spec"].(map[string]interface{})
				initContainers := templateSpec["initContainers"].([]interface{})

				// Should have only one init container remaining (other-init)
				assert.Len(t, initContainers, 1)

				remainingContainer := initContainers[0].(map[string]interface{})
				assert.Equal(t, "other-init", remainingContainer["name"])
			},
		},
		{
			name: "deployment without wait-for-migration-job init container - should remain unchanged",
			itemContent: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata": map[string]interface{}{
					"name":      "test-deployment",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"replicas": 1,
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"containers": []interface{}{
								map[string]interface{}{
									"name":  "app",
									"image": "nginx:latest",
								},
							},
							"initContainers": []interface{}{
								map[string]interface{}{
									"name":  "other-init",
									"image": "busybox:latest",
								},
							},
						},
					},
				},
			},
			expectedError: false,
			validateFn: func(t *testing.T, output *velero.RestoreItemActionExecuteOutput) {
				itemContent := output.UpdatedItem.UnstructuredContent()
				spec := itemContent["spec"].(map[string]interface{})
				template := spec["template"].(map[string]interface{})
				templateSpec := template["spec"].(map[string]interface{})
				initContainers := templateSpec["initContainers"].([]interface{})

				// Should have the same init container (other-init)
				assert.Len(t, initContainers, 1)

				remainingContainer := initContainers[0].(map[string]interface{})
				assert.Equal(t, "other-init", remainingContainer["name"])
			},
		},
		{
			name: "deployment with only wait-for-migration-job init container - should remove initContainers field",
			itemContent: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata": map[string]interface{}{
					"name":      "test-deployment",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"replicas": 1,
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"containers": []interface{}{
								map[string]interface{}{
									"name":  "app",
									"image": "nginx:latest",
								},
							},
							"initContainers": []interface{}{
								map[string]interface{}{
									"name":  MigrationInitContainerName,
									"image": "busybox:latest",
								},
							},
						},
					},
				},
			},
			expectedError: false,
			validateFn: func(t *testing.T, output *velero.RestoreItemActionExecuteOutput) {
				itemContent := output.UpdatedItem.UnstructuredContent()
				spec := itemContent["spec"].(map[string]interface{})
				template := spec["template"].(map[string]interface{})
				templateSpec := template["spec"].(map[string]interface{})

				// initContainers field should be removed entirely
				_, hasInitContainers := templateSpec["initContainers"]
				assert.False(t, hasInitContainers, "initContainers field should be removed when empty")
			},
		},
		{
			name: "deployment without init containers - should remain unchanged",
			itemContent: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata": map[string]interface{}{
					"name":      "test-deployment",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"replicas": 1,
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"containers": []interface{}{
								map[string]interface{}{
									"name":  "app",
									"image": "nginx:latest",
								},
							},
						},
					},
				},
			},
			expectedError: false,
			validateFn: func(t *testing.T, output *velero.RestoreItemActionExecuteOutput) {
				itemContent := output.UpdatedItem.UnstructuredContent()
				spec := itemContent["spec"].(map[string]interface{})
				template := spec["template"].(map[string]interface{})
				templateSpec := template["spec"].(map[string]interface{})

				// Should not have initContainers field
				_, hasInitContainers := templateSpec["initContainers"]
				assert.False(t, hasInitContainers, "initContainers field should not exist")
			},
		},
		{
			name: "deployment with malformed init containers - should handle gracefully",
			itemContent: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata": map[string]interface{}{
					"name":      "test-deployment",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"replicas": 1,
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"containers": []interface{}{
								map[string]interface{}{
									"name":  "app",
									"image": "nginx:latest",
								},
							},
							"initContainers": "not-a-list", // Malformed
						},
					},
				},
			},
			expectedError: false,
			validateFn: func(t *testing.T, output *velero.RestoreItemActionExecuteOutput) {
				// Should return unchanged deployment
				itemContent := output.UpdatedItem.UnstructuredContent()
				spec := itemContent["spec"].(map[string]interface{})
				template := spec["template"].(map[string]interface{})
				templateSpec := template["spec"].(map[string]interface{})
				initContainers := templateSpec["initContainers"]

				// Should remain as the malformed value
				assert.Equal(t, "not-a-list", initContainers)
			},
		},
		{
			name: "deployment with init container without name - should preserve it",
			itemContent: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata": map[string]interface{}{
					"name":      "test-deployment",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"replicas": 1,
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"containers": []interface{}{
								map[string]interface{}{
									"name":  "app",
									"image": "nginx:latest",
								},
							},
							"initContainers": []interface{}{
								map[string]interface{}{
									"image": "busybox:latest", // No name field
								},
								map[string]interface{}{
									"name":  MigrationInitContainerName,
									"image": "busybox:latest",
								},
							},
						},
					},
				},
			},
			expectedError: false,
			validateFn: func(t *testing.T, output *velero.RestoreItemActionExecuteOutput) {
				itemContent := output.UpdatedItem.UnstructuredContent()
				spec := itemContent["spec"].(map[string]interface{})
				template := spec["template"].(map[string]interface{})
				templateSpec := template["spec"].(map[string]interface{})
				initContainers := templateSpec["initContainers"].([]interface{})

				// Should have only one init container remaining (the one without name)
				assert.Len(t, initContainers, 1)

				remainingContainer := initContainers[0].(map[string]interface{})
				_, hasName := remainingContainer["name"]
				assert.False(t, hasName, "Container without name should be preserved")
			},
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
