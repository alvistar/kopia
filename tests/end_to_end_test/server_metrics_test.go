package endtoend_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kopia/kopia/internal/apiclient"
	"github.com/kopia/kopia/internal/serverapi"
	"github.com/kopia/kopia/internal/testlogging"
	"github.com/kopia/kopia/internal/testutil"
	"github.com/kopia/kopia/tests/testenv"
)

func TestServerMetrics(t *testing.T) {
	ctx := testlogging.Context(t)

	runner := testenv.NewInProcRunner(t)
	e := testenv.NewCLITest(t, testenv.RepoFormatNotImportant, runner)

	defer e.RunAndExpectSuccess(t, "repo", "disconnect")

	e.RunAndExpectSuccess(t, "repo", "create", "filesystem", "--path", e.RepoDir, "--override-hostname=fake-hostname", "--override-username=fake-username")

	e.RunAndExpectSuccess(t, "snapshot", "create", sharedTestDataDir1)
	e.RunAndExpectSuccess(t, "snapshot", "create", sharedTestDataDir2)

	var sp testutil.ServerParameters

	wait, _ := e.RunAndProcessStderr(t, sp.ProcessOutput,
		"server", "start",
		"--ui",
		"--address=localhost:0",
		"--random-password",
		"--random-server-control-password",
		"--tls-generate-cert",
		"--tls-generate-rsa-key-size=2048",  // use shorter key size to speed up generation
		"--metrics-listen-addr=localhost:0", // enable metrics
	)

	defer wait()

	t.Logf("detected server parameters %#v", sp)

	cli, err := apiclient.NewKopiaAPIClient(apiclient.Options{
		BaseURL:                             sp.BaseURL,
		Username:                            "kopia",
		Password:                            sp.Password,
		TrustedServerCertificateFingerprint: sp.SHA256Fingerprint,
		LogRequests:                         true,
	})
	require.NoError(t, err)
	require.NoError(t, cli.FetchCSRFTokenForTesting(ctx))

	controlClient, err := apiclient.NewKopiaAPIClient(apiclient.Options{
		BaseURL:                             sp.BaseURL,
		Username:                            "server-control",
		Password:                            sp.ServerControlPassword,
		TrustedServerCertificateFingerprint: sp.SHA256Fingerprint,
		LogRequests:                         true,
	})
	require.NoError(t, err)

	defer serverapi.Shutdown(ctx, controlClient)

	waitUntilServerStarted(ctx, t, controlClient)

	metrics := getMetrics(ctx, t, sp.MetricsAddress)

	// Define the expected paths
	expectedPaths := []string{
		sharedTestDataDir1,
		sharedTestDataDir2,
	}

	// Check if the metrics contain the expected paths
	for _, path := range expectedPaths {
		require.Contains(t, metrics, fmt.Sprintf(`kopia_last_snapshot_dirs{host="fake-hostname",path=%q,username="fake-username"}`, path))
	}

	// Delete snapshot
	e.RunAndExpectSuccess(t, "snapshot", "delete", "--all-snapshots-for-source", sharedTestDataDir2, "--delete")
	e.RunAndExpectSuccess(t, "server", "refresh",
		"--address", sp.BaseURL,
		"--server-cert-fingerprint", sp.SHA256Fingerprint,
		"--server-control-password", sp.ServerControlPassword,
	)

	sourceList, err := serverapi.ListSources(ctx, cli, nil)
	require.NoError(t, err)

	// Check 2nd source is deleted
	require.Len(t, sourceList.Sources, 1)

	metrics = getMetrics(ctx, t, sp.MetricsAddress)
	require.Contains(t, metrics, fmt.Sprintf(`kopia_last_snapshot_dirs{host="fake-hostname",path=%q,username="fake-username"}`, sharedTestDataDir1))
	require.NotContains(t, metrics, fmt.Sprintf(`kopia_last_snapshot_dirs{host="fake-hostname",path=%q,username="fake-username"}`, sharedTestDataDir2))
}

func TestServerMetricsWithGlobalDisablePolicy(t *testing.T) {
	ctx := testlogging.Context(t)

	runner := testenv.NewInProcRunner(t)
	e := testenv.NewCLITest(t, testenv.RepoFormatNotImportant, runner)

	defer e.RunAndExpectSuccess(t, "repo", "disconnect")

	e.RunAndExpectSuccess(t, "repo", "create", "filesystem", "--path", e.RepoDir, "--override-hostname=fake-hostname", "--override-username=fake-username")

	e.RunAndExpectSuccess(t, "policy", "set", "--global", "--expose-metrics", "false")

	e.RunAndExpectSuccess(t, "snapshot", "create", sharedTestDataDir1)
	e.RunAndExpectSuccess(t, "snapshot", "create", sharedTestDataDir2)

	var sp testutil.ServerParameters

	wait, _ := e.RunAndProcessStderr(t, sp.ProcessOutput,
		"server", "start",
		"--ui",
		"--address=localhost:0",
		"--random-password",
		"--random-server-control-password",
		"--tls-generate-cert",
		"--tls-generate-rsa-key-size=2048",  // use shorter key size to speed up generation
		"--metrics-listen-addr=localhost:0", // enable metrics
	)

	defer wait()

	t.Logf("detected server parameters %#v", sp)

	cli, err := apiclient.NewKopiaAPIClient(apiclient.Options{
		BaseURL:                             sp.BaseURL,
		Username:                            "kopia",
		Password:                            sp.Password,
		TrustedServerCertificateFingerprint: sp.SHA256Fingerprint,
		LogRequests:                         true,
	})
	require.NoError(t, err)
	require.NoError(t, cli.FetchCSRFTokenForTesting(ctx))

	controlClient, err := apiclient.NewKopiaAPIClient(apiclient.Options{
		BaseURL:                             sp.BaseURL,
		Username:                            "server-control",
		Password:                            sp.ServerControlPassword,
		TrustedServerCertificateFingerprint: sp.SHA256Fingerprint,
		LogRequests:                         true,
	})
	require.NoError(t, err)

	defer serverapi.Shutdown(ctx, controlClient)

	waitUntilServerStarted(ctx, t, controlClient)

	// Check response on the captured metrics address
	metrics := getMetrics(ctx, t, sp.MetricsAddress)

	// Define the expected paths
	expectedPaths := []string{
		sharedTestDataDir1,
		sharedTestDataDir2,
	}

	// Check if the metrics contain the expected paths
	for _, path := range expectedPaths {
		require.NotContains(t, metrics, fmt.Sprintf(`kopia_last_snapshot_dirs{host="fake-hostname",path=%q,username="fake-username"}`, path))
	}
}

func TestServerMetricsWithSpecificDisablePolicy(t *testing.T) {
	ctx := testlogging.Context(t)

	runner := testenv.NewInProcRunner(t)
	e := testenv.NewCLITest(t, testenv.RepoFormatNotImportant, runner)

	defer e.RunAndExpectSuccess(t, "repo", "disconnect")

	e.RunAndExpectSuccess(t, "repo", "create", "filesystem", "--path", e.RepoDir, "--override-hostname=fake-hostname", "--override-username=fake-username")

	e.RunAndExpectSuccess(t, "policy", "set", "--expose-metrics", "false", sharedTestDataDir2)

	e.RunAndExpectSuccess(t, "snapshot", "create", sharedTestDataDir1)
	e.RunAndExpectSuccess(t, "snapshot", "create", sharedTestDataDir2)

	var sp testutil.ServerParameters

	wait, _ := e.RunAndProcessStderr(t, sp.ProcessOutput,
		"server", "start",
		"--ui",
		"--address=localhost:0",
		"--random-password",
		"--random-server-control-password",
		"--tls-generate-cert",
		"--tls-generate-rsa-key-size=2048",  // use shorter key size to speed up generation
		"--metrics-listen-addr=localhost:0", // enable metrics
	)

	defer wait()

	t.Logf("detected server parameters %#v", sp)

	cli, err := apiclient.NewKopiaAPIClient(apiclient.Options{
		BaseURL:                             sp.BaseURL,
		Username:                            "kopia",
		Password:                            sp.Password,
		TrustedServerCertificateFingerprint: sp.SHA256Fingerprint,
		LogRequests:                         true,
	})
	require.NoError(t, err)
	require.NoError(t, cli.FetchCSRFTokenForTesting(ctx))

	controlClient, err := apiclient.NewKopiaAPIClient(apiclient.Options{
		BaseURL:                             sp.BaseURL,
		Username:                            "server-control",
		Password:                            sp.ServerControlPassword,
		TrustedServerCertificateFingerprint: sp.SHA256Fingerprint,
		LogRequests:                         true,
	})
	require.NoError(t, err)

	defer serverapi.Shutdown(ctx, controlClient)

	waitUntilServerStarted(ctx, t, controlClient)

	// Check response on the captured metrics address
	metrics := getMetrics(ctx, t, sp.MetricsAddress)

	// Only sharedTestDataDir should be present
	require.Contains(t, metrics, fmt.Sprintf(`kopia_last_snapshot_dirs{host="fake-hostname",path=%q,username="fake-username"}`, sharedTestDataDir1))
	require.NotContains(t, metrics, fmt.Sprintf(`kopia_last_snapshot_dirs{host="fake-hostname",path=%q,username="fake-username"}`, sharedTestDataDir2))
}

func getMetrics(ctx context.Context, t *testing.T, baseURL string) string {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+baseURL+"/metrics", http.NoBody)
	require.NoError(t, err)
	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	// Response body should not be empty
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NotEmpty(t, body)

	return string(body)
}