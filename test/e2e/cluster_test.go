package e2e

import (
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	"github.com/argoproj/argo-cd/v3/test/e2e/fixture"
	accountFixture "github.com/argoproj/argo-cd/v3/test/e2e/fixture/account"
	"github.com/argoproj/argo-cd/v3/test/e2e/fixture/app"
	clusterFixture "github.com/argoproj/argo-cd/v3/test/e2e/fixture/cluster"
	"github.com/argoproj/argo-cd/v3/util/errors"
)

func TestClusterList(t *testing.T) {
	fixture.SkipIfAlreadyRun(t)
	defer fixture.RecordTestRun(t)

	last := ""
	expected := fmt.Sprintf(`SERVER                          NAME        VERSION  STATUS      MESSAGE  PROJECT
https://kubernetes.default.svc  in-cluster  %v     Successful           `, fixture.GetVersions(t).ServerVersion)

	clusterFixture.
		Given(t).
		Project(fixture.ProjectName)

	// We need an application targeting the cluster, otherwise the test will
	// fail if run isolated.
	app.GivenWithSameState(t).
		Path(guestbookPath).
		When().
		CreateApp()

	tries := 25
	for i := 0; i <= tries; i++ {
		clusterFixture.GivenWithSameState(t).
			When().
			List().
			Then().
			AndCLIOutput(func(output string, _ error) {
				last = output
			})
		if expected == last {
			break
		} else if i < tries {
			// We retry with a simple backoff
			time.Sleep(time.Duration(i+1) * 100 * time.Millisecond)
		}
	}
	assert.Equal(t, expected, last)
}

func TestClusterAdd(t *testing.T) {
	clusterFixture.
		Given(t).
		Project(fixture.ProjectName).
		Upsert(true).
		Server(KubernetesInternalAPIServerAddr).
		When().
		Create().
		List().
		Then().
		AndCLIOutput(func(output string, _ error) {
			assert.Equal(t, fmt.Sprintf(`SERVER                          NAME              VERSION  STATUS      MESSAGE  PROJECT
https://kubernetes.default.svc  test-cluster-add  %v     Successful           %s`, fixture.GetVersions(t).ServerVersion, fixture.ProjectName), output)
		})
}

func TestClusterAddPermissionDenied(t *testing.T) {
	accountFixture.Given(t).
		Name("test").
		When().
		Create().
		Login().
		SetPermissions([]fixture.ACL{}, "org-admin")

	clusterFixture.
		GivenWithSameState(t).
		Project(fixture.ProjectName).
		Upsert(true).
		Server(KubernetesInternalAPIServerAddr).
		When().
		IgnoreErrors().
		Create().
		Then().
		AndCLIOutput(func(_ string, err error) {
			assert.ErrorContains(t, err, "PermissionDenied desc = permission denied")
		})
}

func TestClusterAddAllowed(t *testing.T) {
	accountFixture.Given(t).
		Name("test").
		When().
		Create().
		Login().
		SetPermissions([]fixture.ACL{
			{
				Resource: "clusters",
				Action:   "create",
				Scope:    fixture.ProjectName + "/*",
			},
			{
				Resource: "clusters",
				Action:   "get",
				Scope:    fixture.ProjectName + "/*",
			},
		}, "org-admin")

	clusterFixture.
		GivenWithSameState(t).
		Project(fixture.ProjectName).
		Upsert(true).
		Server(KubernetesInternalAPIServerAddr).
		When().
		Create().
		List().
		Then().
		AndCLIOutput(func(output string, _ error) {
			assert.Equal(t, fmt.Sprintf(`SERVER                          NAME                      VERSION  STATUS      MESSAGE  PROJECT
https://kubernetes.default.svc  test-cluster-add-allowed  %v     Successful           argo-project`, fixture.GetVersions(t).ServerVersion), output)
		})
}

func TestClusterListDenied(t *testing.T) {
	accountFixture.Given(t).
		Name("test").
		When().
		Create().
		Login().
		SetPermissions([]fixture.ACL{
			{
				Resource: "clusters",
				Action:   "create",
				Scope:    fixture.ProjectName + "/*",
			},
		}, "org-admin")

	clusterFixture.
		GivenWithSameState(t).
		Project(fixture.ProjectName).
		Upsert(true).
		Server(KubernetesInternalAPIServerAddr).
		When().
		Create().
		List().
		Then().
		AndCLIOutput(func(output string, _ error) {
			assert.Equal(t, "SERVER  NAME  VERSION  STATUS  MESSAGE  PROJECT", output)
		})
}

func TestClusterSet(t *testing.T) {
	fixture.EnsureCleanState(t)
	defer fixture.RecordTestRun(t)
	clusterFixture.
		GivenWithSameState(t).
		Project(fixture.ProjectName).
		Name("in-cluster").
		Namespaces([]string{"namespace-edit-1", "namespace-edit-2"}).
		Server(KubernetesInternalAPIServerAddr).
		When().
		SetNamespaces().
		GetByName("in-cluster").
		Then().
		AndCLIOutput(func(output string, _ error) {
			assert.Contains(t, output, "namespace-edit-1")
			assert.Contains(t, output, "namespace-edit-2")
		})
}

func TestClusterGet(t *testing.T) {
	fixture.SkipIfAlreadyRun(t)
	fixture.EnsureCleanState(t)
	defer fixture.RecordTestRun(t)
	output := errors.NewHandler(t).FailOnErr(fixture.RunCli("cluster", "get", "https://kubernetes.default.svc")).(string)

	assert.Contains(t, output, "name: in-cluster")
	assert.Contains(t, output, "server: https://kubernetes.default.svc")
	assert.Contains(t, output, fmt.Sprintf(`serverVersion: "%v"`, fixture.GetVersions(t).ServerVersion))
	assert.Contains(t, output, `config:
  tlsClientConfig:
    insecure: false`)

	assert.Contains(t, output, `status: Successful`)
}

func TestClusterNameInRestAPI(t *testing.T) {
	fixture.EnsureCleanState(t)

	var cluster Cluster
	err := fixture.DoHttpJsonRequest("GET", "/api/v1/clusters/in-cluster?id.type=name", &cluster)
	require.NoError(t, err)

	assert.Equal(t, "in-cluster", cluster.Name)
	assert.Contains(t, cluster.Server, "https://kubernetes.default.svc")

	err = fixture.DoHttpJsonRequest("PUT",
		"/api/v1/clusters/in-cluster?id.type=name&updatedFields=labels", &cluster, []byte(`{"labels":{"test": "val"}}`)...)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"test": "val"}, cluster.Labels)
}

func TestClusterURLInRestAPI(t *testing.T) {
	fixture.EnsureCleanState(t)

	clusterURL := url.QueryEscape(KubernetesInternalAPIServerAddr)

	var cluster Cluster
	err := fixture.DoHttpJsonRequest("GET", "/api/v1/clusters/"+clusterURL, &cluster)
	require.NoError(t, err)

	assert.Equal(t, "in-cluster", cluster.Name)
	assert.Contains(t, cluster.Server, "https://kubernetes.default.svc")

	err = fixture.DoHttpJsonRequest("PUT",
		fmt.Sprintf("/api/v1/clusters/%s?&updatedFields=labels", clusterURL), &cluster, []byte(`{"labels":{"test": "val"}}`)...)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"test": "val"}, cluster.Labels)
}

func TestClusterDeleteDenied(t *testing.T) {
	accountFixture.Given(t).
		Name("test").
		When().
		Create().
		Login().
		SetPermissions([]fixture.ACL{
			{
				Resource: "clusters",
				Action:   "create",
				Scope:    fixture.ProjectName + "/*",
			},
			{
				Resource: "clusters",
				Action:   "get",
				Scope:    fixture.ProjectName + "/*",
			},
		}, "org-admin")

	// Attempt to remove cluster creds by name
	clusterFixture.
		GivenWithSameState(t).
		Project(fixture.ProjectName).
		Upsert(true).
		Server(KubernetesInternalAPIServerAddr).
		When().
		Create().
		DeleteByName().
		Then().
		AndCLIOutput(func(_ string, err error) {
			assert.ErrorContains(t, err, "PermissionDenied desc = permission denied")
		})

	// Attempt to remove cluster creds by server
	clusterFixture.
		GivenWithSameState(t).
		Project(fixture.ProjectName).
		Upsert(true).
		Server(KubernetesInternalAPIServerAddr).
		When().
		Create().
		DeleteByServer().
		Then().
		AndCLIOutput(func(_ string, err error) {
			assert.ErrorContains(t, err, "PermissionDenied desc = permission denied")
		})
}

func TestClusterDelete(t *testing.T) {
	accountFixture.Given(t).
		Name("default").
		When().
		Create().
		Login().
		SetPermissions([]fixture.ACL{
			{
				Resource: "clusters",
				Action:   "create",
				Scope:    fixture.ProjectName + "/*",
			},
			{
				Resource: "clusters",
				Action:   "get",
				Scope:    fixture.ProjectName + "/*",
			},
			{
				Resource: "clusters",
				Action:   "delete",
				Scope:    fixture.ProjectName + "/*",
			},
		}, "org-admin")

	clstAction := clusterFixture.
		GivenWithSameState(t).
		Name("default").
		Project(fixture.ProjectName).
		Upsert(true).
		Server(KubernetesInternalAPIServerAddr).
		When().
		CreateWithRBAC()

	// Check that RBAC is created
	_, err := fixture.Run("", "kubectl", "get", "serviceaccount", "argocd-manager", "-n", "kube-system")
	require.NoError(t, err, "Expected no error from not finding serviceaccount argocd-manager")

	_, err = fixture.Run("", "kubectl", "get", "clusterrole", "argocd-manager-role")
	require.NoError(t, err, "Expected no error from not finding clusterrole argocd-manager-role")

	_, err = fixture.Run("", "kubectl", "get", "clusterrolebinding", "argocd-manager-role-binding")
	require.NoError(t, err, "Expected no error from not finding clusterrolebinding argocd-manager-role-binding")

	clstAction.DeleteByName().
		Then().
		AndCLIOutput(func(output string, _ error) {
			assert.Equal(t, "Cluster 'default' removed", output)
		})

	// Check that RBAC is removed after delete
	output, err := fixture.Run("", "kubectl", "get", "serviceaccount", "argocd-manager", "-n", "kube-system")
	require.Error(t, err, "Expected error from not finding serviceaccount argocd-manager but got:\n%s", output)

	output, err = fixture.Run("", "kubectl", "get", "clusterrole", "argocd-manager-role")
	require.Error(t, err, "Expected error from not finding clusterrole argocd-manager-role but got:\n%s", output)

	output, err = fixture.Run("", "kubectl", "get", "clusterrolebinding", "argocd-manager-role-binding")
	assert.Error(t, err, "Expected error from not finding clusterrolebinding argocd-manager-role-binding but got:\n%s", output)
}
