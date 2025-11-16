package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	// Test for building manager options
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	logr "github.com/go-logr/logr"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	config "sigs.k8s.io/controller-runtime/pkg/config"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	// ctrl alias not required; we use manager.Runnable from pkg/manager
)

// A small fake manager used to test healthCheck.
type testMgr struct {
	client client.Client
	mapper apimeta.RESTMapper
}

func (t *testMgr) GetClient() client.Client          { return t.client }
func (t *testMgr) GetAPIReader() client.Reader       { return t.client }
func (t *testMgr) GetRESTMapper() apimeta.RESTMapper { return t.mapper }

// A minimal fake manager for tests in this package
type fakeManager struct {
	client client.Client
	scheme *runtime.Scheme
}

func (f *fakeManager) GetClient() client.Client             { return f.client }
func (f *fakeManager) GetScheme() *runtime.Scheme           { return f.scheme }
func (f *fakeManager) GetConfig() *rest.Config              { return &rest.Config{} }
func (f *fakeManager) GetHTTPClient() *http.Client          { return &http.Client{} }
func (f *fakeManager) GetCache() cache.Cache                { return nil }
func (f *fakeManager) GetFieldIndexer() client.FieldIndexer { return nil }
func (f *fakeManager) GetEventRecorderFor(name string) record.EventRecorder {
	return record.NewFakeRecorder(10)
}
func (f *fakeManager) GetRESTMapper() apimeta.RESTMapper {
	return apimeta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "", Version: "v1"}})
}
func (f *fakeManager) GetAPIReader() client.Reader     { return f.client }
func (f *fakeManager) Start(ctx context.Context) error { return nil }

func (f *fakeManager) Add(r manager.Runnable) error { return nil }
func (f *fakeManager) Elected() <-chan struct{}     { return make(chan struct{}) }
func (f *fakeManager) AddMetricsServerExtraHandler(path string, handler http.Handler) error {
	return nil
}
func (f *fakeManager) AddHealthzCheck(name string, check healthz.Checker) error { return nil }
func (f *fakeManager) AddReadyzCheck(name string, check healthz.Checker) error  { return nil }
func (f *fakeManager) GetWebhookServer() webhook.Server                         { return nil }
func (f *fakeManager) GetLogger() logr.Logger                                   { return logr.Discard() }
func (f *fakeManager) GetControllerOptions() config.Controller                  { return config.Controller{} }

// The rest of the manager methods are not used by healthCheck; add stubs to satisfy interface
func (t *testMgr) GetScheme() *runtime.Scheme                                           { return runtime.NewScheme() }
func (t *testMgr) GetConfig() *rest.Config                                              { return &rest.Config{} }
func (t *testMgr) GetHTTPClient() *http.Client                                          { return &http.Client{} }
func (t *testMgr) GetFieldIndexer() client.FieldIndexer                                 { return nil }
func (t *testMgr) GetEventRecorderFor(s string) record.EventRecorder                    { return nil }
func (t *testMgr) GetCache() cache.Cache                                                { return nil }
func (t *testMgr) Start(ctx context.Context) error                                      { return nil }
func (t *testMgr) Add(r manager.Runnable) error                                         { return nil }
func (t *testMgr) Elected() <-chan struct{}                                             { return make(chan struct{}) }
func (t *testMgr) AddMetricsServerExtraHandler(path string, handler http.Handler) error { return nil }
func (t *testMgr) AddHealthzCheck(name string, check healthz.Checker) error             { return nil }
func (t *testMgr) AddReadyzCheck(name string, check healthz.Checker) error              { return nil }
func (t *testMgr) GetWebhookServer() webhook.Server                                     { return nil }
func (t *testMgr) GetLogger() logr.Logger                                               { return logr.Discard() }
func (t *testMgr) GetControllerOptions() config.Controller                              { return config.Controller{} }

// GetRESTMapper already defined above

// A client that returns an error when listing
type listErrorClient struct {
	client.Client
	listErr error
}

func (c *listErrorClient) List(ctx context.Context, l client.ObjectList, opts ...client.ListOption) error {
	if c.listErr != nil {
		return c.listErr
	}
	return c.Client.List(ctx, l, opts...)
}

func TestHealthCheck_Success(t *testing.T) {
	scheme := runtime.NewScheme()
	// add core types so RESTMapper and client can work
	_ = corev1.AddToScheme(scheme)

	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	// Create a fake ConfigMap in default namespace
	obj := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"}}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(obj).Build()

	mapper := apimeta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "", Version: "v1"}})
	mapper.Add(gvk, apimeta.RESTScopeNamespace)

	mgr := &testMgr{client: cl, mapper: mapper}

	req := new(http.Request)
	req = req.WithContext(context.Background())
	if err := healthCheck(req, mgr, gvk); err != nil {
		t.Fatalf("expected success from healthCheck, got error: %v", err)
	}
}

func TestHealthCheck_RESTMappingError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}

	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	// mapper with no mapping for gvk
	mapper := apimeta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "", Version: "v1"}})

	mgr := &testMgr{client: cl, mapper: mapper}
	req := new(http.Request)
	req = req.WithContext(context.Background())
	if err := healthCheck(req, mgr, gvk); err == nil {
		t.Fatalf("expected healthCheck to fail when mapping missing")
	}
}

func TestHealthCheck_ListFails(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	// build a real client but wrap it so List returns an error
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "xxx"}}).Build()
	cl := &listErrorClient{Client: base, listErr: errors.New("boom")}

	mapper := apimeta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "", Version: "v1"}})
	mapper.Add(gvk, apimeta.RESTScopeNamespace)

	mgr := &testMgr{client: cl, mapper: mapper}

	req := new(http.Request)
	req = req.WithContext(context.Background())
	if err := healthCheck(req, mgr, gvk); err == nil {
		t.Fatalf("expected healthCheck to return error when List fails")
	}
}

func TestHealthCheck_ClusterScoped(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	// Node is cluster-scoped. Add a Node object so list succeeds
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Node"}
	obj := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(obj).Build()

	mapper := apimeta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "", Version: "v1"}})
	// Add Node mapping as cluster-scoped
	mapper.Add(gvk, apimeta.RESTScopeRoot)

	mgr := &testMgr{client: cl, mapper: mapper}

	req := new(http.Request)
	req = req.WithContext(context.Background())
	if err := healthCheck(req, mgr, gvk); err != nil {
		t.Fatalf("expected success from healthCheck for cluster-scoped resource, got error: %v", err)
	}
}

func TestParseLeaderElection_InvalidBool(t *testing.T) {
	os.Setenv("LEASE_LEADER_ELECTION", "notabool")
	defer os.Unsetenv("LEASE_LEADER_ELECTION")
	_, _, err := parseLeaderElectionConfig(false, "")
	if err == nil {
		t.Fatalf("expected error on invalid LEASE_LEADER_ELECTION value")
	}
}

func TestParseLeaderElection_EnabledNoNamespaceOffline(t *testing.T) {
	os.Setenv("LEASE_LEADER_ELECTION", "true")
	defer os.Unsetenv("LEASE_LEADER_ELECTION")

	// Simulate running outside cluster: stat returns IsNotExist
	oldStat := statFn
	defer func() { statFn = oldStat }()
	statFn = func(name string) (os.FileInfo, error) { return nil, os.ErrNotExist }

	_, _, err := parseLeaderElectionConfig(false, "")
	if err == nil {
		t.Fatalf("expected error when leader election enabled but no namespace in env and not in cluster")
	}
}

func TestParseLeaderElection_UsesEnvNamespace(t *testing.T) {
	os.Setenv("LEASE_LEADER_ELECTION", "true")
	os.Setenv("LEASE_LEADER_ELECTION_NAMESPACE", "myns")
	defer func() { os.Unsetenv("LEASE_LEADER_ELECTION"); os.Unsetenv("LEASE_LEADER_ELECTION_NAMESPACE") }()

	enabled, ns, err := parseLeaderElectionConfig(false, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !enabled || ns != "myns" {
		t.Fatalf("expected enabled=true ns=myns, got enabled=%v ns=%q", enabled, ns)
	}
}

func TestParseLeaderElection_DefaultsToServiceAccountNamespace(t *testing.T) {
	os.Setenv("LEASE_LEADER_ELECTION", "true")
	defer os.Unsetenv("LEASE_LEADER_ELECTION")

	oldStat := statFn
	oldRead := readFileFn
	defer func() { statFn = oldStat; readFileFn = oldRead }()
	// Simulate presence of namespace file in cluster
	statFn = func(name string) (os.FileInfo, error) { return nil, nil }
	readFileFn = func(name string) ([]byte, error) { return []byte("svcns\n"), nil }

	enabled, ns, err := parseLeaderElectionConfig(false, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !enabled || ns != "svcns" {
		t.Fatalf("expected enabled=true ns=svcns, got enabled=%v ns=%q", enabled, ns)
	}
}

func TestMain_Run(t *testing.T) {
	// Save global state
	oldArgs := os.Args
	oldNewMgr := newManager
	oldGetConfig := getConfig
	t.Cleanup(func() {
		os.Args = oldArgs
		newManager = oldNewMgr
		getConfig = oldGetConfig
	})

	// Minimal fake manager
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	mov := &fakeManager{client: fake.NewClientBuilder().WithScheme(scheme).Build(), scheme: scheme}

	// Override getConfig to return a fake config
	getConfig = func() *rest.Config {
		return &rest.Config{}
	}

	// Override newManager to return our fake manager
	newManager = func(cfg *rest.Config, opts ctrl.Options) (ctrl.Manager, error) {
		return mov, nil
	}

	// Run main with a valid gvk so it proceeds and returns
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	os.Args = []string{"cmd", "-group=apps", "-version=v1", "-kind=ConfigMap"}
	main()
}

func TestRun_ExitsOnMissingGVK(t *testing.T) {
	oldExit := exitFn
	t.Cleanup(func() { exitFn = oldExit })

	exitFn = func(code int) { panic(fmt.Sprintf("exited %d", code)) }

	params := ParseParams{} // all empty
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected exit via exitFn")
		}
	}()
	run(params)
}

func TestRun_ParseLeaderElectionInvalid(t *testing.T) {
	oldExit := exitFn
	t.Cleanup(func() { exitFn = oldExit })

	os.Setenv("LEASE_LEADER_ELECTION", "notabool")
	defer os.Unsetenv("LEASE_LEADER_ELECTION")

	exitFn = func(code int) { panic(fmt.Sprintf("exited %d", code)) }

	params := ParseParams{Group: "apps", Version: "v1", Kind: "ConfigMap"}
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected exit via exitFn for invalid leader election env")
		}
	}()
	run(params)
}

func TestRun_MetricsHealthReadyStartPaths(t *testing.T) {
	oldNew := newManager
	oldExit := exitFn
	oldGetConfig := getConfig
	t.Cleanup(func() {
		newManager = oldNew
		exitFn = oldExit
		getConfig = oldGetConfig
	})

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	// Mock getConfig
	getConfig = func() *rest.Config {
		return &rest.Config{}
	}

	// metrics error
	mov1 := &failAlertsManager{fakeManager{client: fake.NewClientBuilder().WithScheme(scheme).Build(), scheme: scheme}}
	newManager = func(cfg *rest.Config, opts ctrl.Options) (ctrl.Manager, error) { return mov1, nil }
	exitFn = func(code int) { panic(fmt.Sprintf("exited %d", code)) }
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected exit via exitFn for metrics error")
		}
	}()
	run(ParseParams{Group: "apps", Version: "v1", Kind: "ConfigMap"})
}

func TestRun_HealthzError(t *testing.T) {
	oldNew := newManager
	oldExit := exitFn
	oldGetConfig := getConfig
	t.Cleanup(func() {
		newManager = oldNew
		exitFn = oldExit
		getConfig = oldGetConfig
	})
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	getConfig = func() *rest.Config {
		return &rest.Config{}
	}
	mov := &failHealthzManager{fakeManager{client: fake.NewClientBuilder().WithScheme(scheme).Build(), scheme: scheme}}
	newManager = func(cfg *rest.Config, opts ctrl.Options) (ctrl.Manager, error) { return mov, nil }
	exitFn = func(code int) { panic(fmt.Sprintf("exited %d", code)) }
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected exit via exitFn for healthz error")
		}
	}()
	run(ParseParams{Group: "apps", Version: "v1", Kind: "ConfigMap"})
}

func TestRun_ReadyzError(t *testing.T) {
	oldNew := newManager
	oldExit := exitFn
	oldGetConfig := getConfig
	t.Cleanup(func() {
		newManager = oldNew
		exitFn = oldExit
		getConfig = oldGetConfig
	})
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	getConfig = func() *rest.Config {
		return &rest.Config{}
	}
	mov := &failReadyzManager{fakeManager{client: fake.NewClientBuilder().WithScheme(scheme).Build(), scheme: scheme}}
	newManager = func(cfg *rest.Config, opts ctrl.Options) (ctrl.Manager, error) { return mov, nil }
	exitFn = func(code int) { panic(fmt.Sprintf("exited %d", code)) }
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected exit via exitFn for readyz error")
		}
	}()
	run(ParseParams{Group: "apps", Version: "v1", Kind: "ConfigMap"})
}

func TestRun_ConfigureNamespaceReconcilerSetupFails(t *testing.T) {
	oldNew := newManager
	oldGetConfig := getConfig
	t.Cleanup(func() {
		newManager = oldNew
		getConfig = oldGetConfig
	})
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	getConfig = func() *rest.Config {
		return &rest.Config{}
	}
	mov := &errAddManager{fakeManager{client: fake.NewClientBuilder().WithScheme(scheme).Build(), scheme: scheme}}
	newManager = func(cfg *rest.Config, opts ctrl.Options) (ctrl.Manager, error) { return mov, nil }

	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic when configureNamespaceReconciler fails")
		}
	}()
	run(ParseParams{Group: "apps", Version: "v1", Kind: "ConfigMap", OptInLabelKey: "watch/enabled", OptInLabelValue: "true"})
}

func TestRun_LWSetupFailsPanics(t *testing.T) {
	oldNew := newManager
	oldGetConfig := getConfig
	t.Cleanup(func() {
		newManager = oldNew
		getConfig = oldGetConfig
	})
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	getConfig = func() *rest.Config {
		return &rest.Config{}
	}
	mov := &errAddManager{fakeManager{client: fake.NewClientBuilder().WithScheme(scheme).Build(), scheme: scheme}}
	newManager = func(cfg *rest.Config, opts ctrl.Options) (ctrl.Manager, error) { return mov, nil }

	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic when LeaseWatcher.SetupWithManager fails")
		}
	}()
	run(ParseParams{Group: "apps", Version: "v1", Kind: "ConfigMap"})
}

func TestRun_StartFailsPanic(t *testing.T) {
	oldNew := newManager
	oldGetConfig := getConfig
	t.Cleanup(func() {
		newManager = oldNew
		getConfig = oldGetConfig
	})
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	getConfig = func() *rest.Config {
		return &rest.Config{}
	}
	mov := &startFailManager{fakeManager{client: fake.NewClientBuilder().WithScheme(scheme).Build(), scheme: scheme}}
	newManager = func(cfg *rest.Config, opts ctrl.Options) (ctrl.Manager, error) { return mov, nil }
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic when Start returns error")
		}
	}()
	run(ParseParams{Group: "apps", Version: "v1", Kind: "ConfigMap"})
}

// TestParseLeaderElection_FlagEnabledUsesProvidedNamespace checks parse when flag
func TestParseLeaderElection_EnvFalse(t *testing.T) {
	os.Setenv("LEASE_LEADER_ELECTION", "false")
	defer os.Unsetenv("LEASE_LEADER_ELECTION")

	enabled, ns, err := parseLeaderElectionConfig(false, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enabled || ns != "" {
		t.Fatalf("expected leader election disabled, got enabled=%v ns=%q", enabled, ns)
	}
}

func TestMain_ExitsOnMissingGVK(t *testing.T) {
	// Save global state
	oldArgs := os.Args
	oldExit := exitFn
	oldGetConfig := getConfig
	t.Cleanup(func() {
		os.Args = oldArgs
		exitFn = oldExit
		getConfig = oldGetConfig
	})

	getConfig = func() *rest.Config {
		return &rest.Config{}
	}
	exitFn = func(code int) { panic(fmt.Sprintf("exited %d", code)) }
	// Ensure no flags or envs provide GVK
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	os.Unsetenv("LEASE_GVK_GROUP")
	os.Unsetenv("LEASE_GVK_VERSION")
	os.Unsetenv("LEASE_GVK_KIND")
	os.Args = []string{"cmd"}
	// Ensure parseParameters returns what we expect
	p := parseParameters()
	t.Logf("parseParameters returned %+v", p)
	// Ensure no environment variables are set that could satisfy the GVK
	os.Unsetenv("LEASE_GVK_GROUP")
	os.Unsetenv("LEASE_GVK_VERSION")
	os.Unsetenv("LEASE_GVK_KIND")
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected main to panic via exitFn")
		}
	}()
	main()
}

func TestMain_NewManagerErrorPanics(t *testing.T) {
	oldArgs := os.Args
	oldNew := newManager
	oldGetConfig := getConfig
	t.Cleanup(func() {
		os.Args = oldArgs
		newManager = oldNew
		getConfig = oldGetConfig
	})

	// Mock getConfig
	getConfig = func() *rest.Config {
		return &rest.Config{}
	}

	// newManager returns an error to trigger panic
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	newManager = func(cfg *rest.Config, opts ctrl.Options) (ctrl.Manager, error) { return nil, fmt.Errorf("boom") }
	os.Args = []string{"cmd", "-group=apps", "-version=v1", "-kind=ConfigMap"}

	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic on NewManager error")
		}
	}()
	main()
}

// manager that returns errors from healthz/readyz/metrics to test exit

type failAlertsManager struct{ fakeManager }

func (f *failAlertsManager) AddHealthzCheck(name string, check healthz.Checker) error {
	return fmt.Errorf("boom")
}
func (f *failAlertsManager) AddReadyzCheck(name string, check healthz.Checker) error {
	return fmt.Errorf("boom")
}
func (f *failAlertsManager) AddMetricsServerExtraHandler(path string, handler http.Handler) error {
	return fmt.Errorf("boom")
}

type failHealthzManager struct{ fakeManager }

func (f *failHealthzManager) AddHealthzCheck(name string, check healthz.Checker) error {
	return fmt.Errorf("boom")
}

type failReadyzManager struct{ fakeManager }

func (f *failReadyzManager) AddReadyzCheck(name string, check healthz.Checker) error {
	return fmt.Errorf("boom")
}

type startFailManager struct{ fakeManager }

func (s *startFailManager) Start(ctx context.Context) error { return fmt.Errorf("boom") }

func TestMain_AddMetricsErrorExits(t *testing.T) {
	oldArgs := os.Args
	oldNew := newManager
	oldExit := exitFn
	oldGetConfig := getConfig
	t.Cleanup(func() {
		os.Args = oldArgs
		newManager = oldNew
		exitFn = oldExit
		getConfig = oldGetConfig
	})

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	getConfig = func() *rest.Config {
		return &rest.Config{}
	}
	mov := &failAlertsManager{fakeManager{client: fake.NewClientBuilder().WithScheme(scheme).Build(), scheme: scheme}}
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	newManager = func(cfg *rest.Config, opts ctrl.Options) (ctrl.Manager, error) { return mov, nil }

	exitFn = func(code int) { panic(fmt.Sprintf("exited %d", code)) }

	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	os.Args = []string{"cmd", "-group=apps", "-version=v1", "-kind=ConfigMap"}
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic via exitFn for AddMetricsServerExtraHandler error")
		}
	}()
	main()
}

func TestMain_HealthzErrorExits(t *testing.T) {
	oldArgs := os.Args
	oldNew := newManager
	oldExit := exitFn
	oldGetConfig := getConfig
	t.Cleanup(func() {
		os.Args = oldArgs
		newManager = oldNew
		exitFn = oldExit
		getConfig = oldGetConfig
	})

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	getConfig = func() *rest.Config {
		return &rest.Config{}
	}
	mov := &failHealthzManager{fakeManager{client: fake.NewClientBuilder().WithScheme(scheme).Build(), scheme: scheme}}
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	newManager = func(cfg *rest.Config, opts ctrl.Options) (ctrl.Manager, error) { return mov, nil }

	exitFn = func(code int) { panic(fmt.Sprintf("exited %d", code)) }

	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	os.Args = []string{"cmd", "-group=apps", "-version=v1", "-kind=ConfigMap"}
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic via exitFn for AddHealthzCheck error")
		}
	}()
	main()
}

func TestMain_ReadyzErrorExits(t *testing.T) {
	oldArgs := os.Args
	oldNew := newManager
	oldExit := exitFn
	oldGetConfig := getConfig
	t.Cleanup(func() {
		os.Args = oldArgs
		newManager = oldNew
		exitFn = oldExit
		getConfig = oldGetConfig
	})

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	getConfig = func() *rest.Config {
		return &rest.Config{}
	}
	mov := &failReadyzManager{fakeManager{client: fake.NewClientBuilder().WithScheme(scheme).Build(), scheme: scheme}}
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	newManager = func(cfg *rest.Config, opts ctrl.Options) (ctrl.Manager, error) { return mov, nil }

	exitFn = func(code int) { panic(fmt.Sprintf("exited %d", code)) }

	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	os.Args = []string{"cmd", "-group=apps", "-version=v1", "-kind=ConfigMap"}
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic via exitFn for AddReadyzCheck error")
		}
	}()
	main()
}

func TestMain_StartFailsPanics(t *testing.T) {
	oldArgs := os.Args
	oldNew := newManager
	oldGetConfig := getConfig
	t.Cleanup(func() {
		os.Args = oldArgs
		newManager = oldNew
		getConfig = oldGetConfig
	})

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	getConfig = func() *rest.Config {
		return &rest.Config{}
	}
	mov := &startFailManager{fakeManager{client: fake.NewClientBuilder().WithScheme(scheme).Build(), scheme: scheme}}
	newManager = func(cfg *rest.Config, opts ctrl.Options) (ctrl.Manager, error) { return mov, nil }

	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	os.Args = []string{"cmd", "-group=apps", "-version=v1", "-kind=ConfigMap"}

	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic when Start returns error")
		}
	}()
	main()
}

func TestMain_ParseLeaderElectionConfigInvalidEnv(t *testing.T) {
	oldArgs := os.Args
	oldExit := exitFn
	oldGetConfig := getConfig
	t.Cleanup(func() {
		os.Args = oldArgs
		exitFn = oldExit
		getConfig = oldGetConfig
	})

	os.Setenv("LEASE_LEADER_ELECTION", "notabool")
	defer os.Unsetenv("LEASE_LEADER_ELECTION")

	getConfig = func() *rest.Config {
		return &rest.Config{}
	}
	exitFn = func(code int) { panic(fmt.Sprintf("exited %d", code)) }

	os.Args = []string{"cmd", "-group=apps", "-version=v1", "-kind=ConfigMap"}
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic via exitFn for invalid LEASE_LEADER_ELECTION")
		}
	}()
	main()
}

func TestMain_LWSetupFailsPanics(t *testing.T) {
	oldArgs := os.Args
	oldNew := newManager
	oldGetConfig := getConfig
	t.Cleanup(func() {
		os.Args = oldArgs
		newManager = oldNew
		getConfig = oldGetConfig
	})

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	getConfig = func() *rest.Config {
		return &rest.Config{}
	}
	// Use a manager where Add returns error so SetupWithManager fails
	mov := &errAddManager{fakeManager{client: fake.NewClientBuilder().WithScheme(scheme).Build(), scheme: scheme}}
	newManager = func(cfg *rest.Config, opts ctrl.Options) (ctrl.Manager, error) { return mov, nil }

	os.Args = []string{"cmd", "-group=apps", "-version=v1", "-kind=ConfigMap"}
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic when LeaseWatcher.SetupWithManager fails")
		}
	}()
	main()
}

func TestParseLeaderElection_EnvTrueWithProvidedParamNamespace(t *testing.T) {
	os.Setenv("LEASE_LEADER_ELECTION", "true")
	defer os.Unsetenv("LEASE_LEADER_ELECTION")

	enabled, ns, err := parseLeaderElectionConfig(false, "provided-ns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !enabled || ns != "provided-ns" {
		t.Fatalf("expected enabled=true ns=provided-ns, got enabled=%v ns=%q", enabled, ns)
	}
}

func TestHealthCheck_UsesLeaseNamespaceEnv(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	obj := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "myns"}}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(obj).Build()

	mapper := apimeta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "", Version: "v1"}})
	mapper.Add(gvk, apimeta.RESTScopeNamespace)

	mgr := &testMgr{client: cl, mapper: mapper}

	os.Setenv("LEASE_NAMESPACE", "myns")
	defer os.Unsetenv("LEASE_NAMESPACE")

	req := new(http.Request)
	req = req.WithContext(context.Background())
	if err := healthCheck(req, mgr, gvk); err != nil {
		t.Fatalf("expected success from healthCheck with LEASE_NAMESPACE override, got error: %v", err)
	}
}

func TestParseLeaderElection_FlagEnabledUsesProvidedNamespace(t *testing.T) {
	// When enabled via flag, should not attempt env parse and should return the passed namespace
	enabled, ns, err := parseLeaderElectionConfig(true, "mypfx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !enabled || ns != "mypfx" {
		t.Fatalf("expected enabled=true ns=mypfx, got enabled=%v ns=%q", enabled, ns)
	}
}

func TestBuildManagerOptions(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	opts := buildManagerOptions(scheme, "apps", "v1", "Deployment", ":8080", ":8081", ":6060", true, "myns")
	if opts.LeaderElection != true {
		t.Fatalf("expected leader election true")
	}
	if opts.LeaderElectionNamespace != "myns" {
		t.Fatalf("expected leaderElectionNamespace myns, got %q", opts.LeaderElectionNamespace)
	}
	if opts.Metrics.BindAddress != ":8080" {
		t.Fatalf("metrics bind address mismatch")
	}
	if opts.PprofBindAddress != ":6060" {
		t.Fatalf("pprof bind address mismatch")
	}
	if opts.HealthProbeBindAddress != ":8081" {
		t.Fatalf("probe bind address mismatch")
	}
	// assert LeaderElectionID contains group/version/kind
	if !strings.Contains(opts.LeaderElectionID, "apps") || !strings.Contains(opts.LeaderElectionID, "v1") || !strings.Contains(opts.LeaderElectionID, "deployment") {
		t.Fatalf("leaderElectionID %q does not contain group/version/kind", opts.LeaderElectionID)
	}
}

func TestNewLeaseWatcher(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	// minimal fake manager
	mov := &fakeManager{client: fake.NewClientBuilder().WithScheme(scheme).Build(), scheme: scheme}

	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	lw := newLeaseWatcher(mov, gvk, "lid")
	if lw == nil {
		t.Fatalf("newLeaseWatcher returned nil")
	}
	if lw.GVK != gvk {
		t.Fatalf("unexpected GVK: %v", lw.GVK)
	}
	if lw.Metrics == nil {
		t.Fatalf("expected metrics to be initialized")
	}
	if lw.Client == nil || lw.Recorder == nil {
		t.Fatalf("expected client and recorder to be set")
	}
}

func TestConfigureNamespaceReconciler(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	mov := &fakeManager{client: fake.NewClientBuilder().WithScheme(scheme).Build(), scheme: scheme}

	// empty labels -> no tracker
	tr, err := configureNamespaceReconciler(mov, "", "", "lid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr != nil {
		t.Fatalf("expected no tracker when opt-in not provided")
	}

	// With labels, expect tracker returned
	tr2, err := configureNamespaceReconciler(mov, "watch/enabled", "true", "lid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr2 == nil {
		t.Fatalf("expected tracker when opt-in provided")
	}
}

// A manager that returns an error from Add so SetupWithManager fails
type errAddManager struct{ fakeManager }

func (e *errAddManager) Add(r manager.Runnable) error { return fmt.Errorf("boom") }

func TestConfigureNamespaceReconciler_SetupError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	mov := &errAddManager{fakeManager{client: fake.NewClientBuilder().WithScheme(scheme).Build(), scheme: scheme}}

	// With labels, SetupWithManager should return an error due to manager Add failure
	tr2, err := configureNamespaceReconciler(mov, "watch/enabled", "true", "lid")
	if err == nil {
		t.Fatalf("expected error when manager Add fails")
	}
	if tr2 != nil {
		t.Fatalf("expected nil tracker when SetupWithManager fails")
	}
}

func TestParseParameters_FromFlags(t *testing.T) {
	// Save global state
	oldArgs := os.Args
	oldFlags := flag.CommandLine
	t.Cleanup(func() { os.Args = oldArgs; flag.CommandLine = oldFlags })

	// Reset flags and set args as if provided via command line
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	os.Args = []string{"cmd",
		"-group=apps",
		"-version=v1",
		"-kind=ConfigMap",
		"-opt-in-label-key=watch/enabled",
		"-opt-in-label-value=true",
		"-metrics-bind-address=:9090",
		"-health-probe-bind-address=:8082",
		"-pprof-bind-address=:6061",
		"-leader-elect=true",
		"-leader-elect-namespace=ldns",
	}

	params := parseParameters()

	if params.Group != "apps" || params.Version != "v1" || params.Kind != "ConfigMap" {
		t.Fatalf("unexpected gvk: %s/%s/%s", params.Group, params.Version, params.Kind)
	}
	if params.OptInLabelKey != "watch/enabled" || params.OptInLabelValue != "true" {
		t.Fatalf("unexpected opt-in labels: %s=%s", params.OptInLabelKey, params.OptInLabelValue)
	}
	if params.MetricsBindAddress != ":9090" || params.HealthProbeBindAddress != ":8082" || params.PprofBindAddress != ":6061" {
		t.Fatalf("unexpected addresses: %s %s %s", params.MetricsBindAddress, params.HealthProbeBindAddress, params.PprofBindAddress)
	}
	if params.LeaderElectionEnabled != true || params.LeaderElectionNamespace != "ldns" {
		t.Fatalf("unexpected leader election flags: enabled=%v ns=%s", params.LeaderElectionEnabled, params.LeaderElectionNamespace)
	}
}

func TestParseParameters_FromEnv(t *testing.T) {
	// Save/restore env
	old := map[string]string{
		"LEASE_GVK_GROUP":                 os.Getenv("LEASE_GVK_GROUP"),
		"LEASE_GVK_VERSION":               os.Getenv("LEASE_GVK_VERSION"),
		"LEASE_GVK_KIND":                  os.Getenv("LEASE_GVK_KIND"),
		"LEASE_OPT_IN_LABEL_KEY":          os.Getenv("LEASE_OPT_IN_LABEL_KEY"),
		"LEASE_OPT_IN_LABEL_VALUE":        os.Getenv("LEASE_OPT_IN_LABEL_VALUE"),
		"LEASE_LEADER_ELECTION":           os.Getenv("LEASE_LEADER_ELECTION"),
		"LEASE_LEADER_ELECTION_NAMESPACE": os.Getenv("LEASE_LEADER_ELECTION_NAMESPACE"),
	}
	t.Cleanup(func() {
		for k, v := range old {
			if v == "" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, v)
			}

		}
		flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
		// When enabled via flag, should not attempt env parse and should return the passed namespace
		enabled, ns, err := parseLeaderElectionConfig(true, "mypfx")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !enabled || ns != "mypfx" {
			t.Fatalf("expected enabled=true ns=mypfx, got enabled=%v ns=%q", enabled, ns)
		}
		os.Args = []string{"cmd"}
	})

	os.Setenv("LEASE_GVK_GROUP", "apps")
	os.Setenv("LEASE_GVK_VERSION", "v1")
	os.Setenv("LEASE_GVK_KIND", "ConfigMap")
	os.Setenv("LEASE_OPT_IN_LABEL_KEY", "watch/enabled")
	os.Setenv("LEASE_OPT_IN_LABEL_VALUE", "true")
	os.Setenv("LEASE_LEADER_ELECTION", "true")
	os.Setenv("LEASE_LEADER_ELECTION_NAMESPACE", "envns")

	// Reset flags and args
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	os.Args = []string{"cmd"}

	params := parseParameters()

	if params.Group != "apps" || params.Version != "v1" || params.Kind != "ConfigMap" {
		t.Fatalf("unexpected gvk from env: %s/%s/%s", params.Group, params.Version, params.Kind)
	}
	if params.OptInLabelKey != "watch/enabled" || params.OptInLabelValue != "true" {
		t.Fatalf("unexpected opt-in from env: %s=%s", params.OptInLabelKey, params.OptInLabelValue)
	}
	if params.LeaderElectionEnabled != true || params.LeaderElectionNamespace != "envns" {
		t.Fatalf("unexpected leader from env: %v %s", params.LeaderElectionEnabled, params.LeaderElectionNamespace)
	}

	// metrics defaults should be present when not specified via flags
	if params.MetricsBindAddress == "" || params.HealthProbeBindAddress == "" || params.PprofBindAddress == "" {
		t.Fatalf("expected default addresses to be set, got metrics=%q probe=%q pprof=%q", params.MetricsBindAddress, params.HealthProbeBindAddress, params.PprofBindAddress)
	}
}

func TestParseParameters_LeaderElectionFlagPrecedence(t *testing.T) {
	// Save global state
	oldArgs := os.Args
	oldFlags := flag.CommandLine
	t.Cleanup(func() { os.Args = oldArgs; flag.CommandLine = oldFlags })

	// Set env to false but pass flag that enables leader election
	os.Setenv("LEASE_LEADER_ELECTION", "false")
	defer os.Unsetenv("LEASE_LEADER_ELECTION")

	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	os.Args = []string{"cmd", "-leader-elect=true"}

	params := parseParameters()
	if !params.LeaderElectionEnabled {
		t.Fatalf("expected leader election enabled due to flag, got false")
	}
}

func TestParseParameters_LeaderElectionEnvOneValue(t *testing.T) {
	// Save global state
	oldArgs := os.Args
	oldFlags := flag.CommandLine
	t.Cleanup(func() { os.Args = oldArgs; flag.CommandLine = oldFlags })

	// Set env to numeric truthy value
	os.Setenv("LEASE_LEADER_ELECTION", "1")
	defer os.Unsetenv("LEASE_LEADER_ELECTION")

	// Reset flags with no leader-elect set
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	os.Args = []string{"cmd"}

	params := parseParameters()
	if !params.LeaderElectionEnabled {
		t.Fatalf("expected leader election enabled due to LEASE_LEADER_ELECTION=1, got false")
	}
}
