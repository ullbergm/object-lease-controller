package main

import (
	"expvar"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	controllers "object-lease-controller/pkg/controllers"
	ometrics "object-lease-controller/pkg/metrics"
	"object-lease-controller/pkg/util"
)

// Lease annotation keys
const (
	AnnTTL        = "object-lease-controller.ullberg.io/ttl"
	AnnLeaseStart = "object-lease-controller.ullberg.io/lease-start" // RFC3339 UTC
	AnnExpireAt   = "object-lease-controller.ullberg.io/expire-at"
	AnnStatus     = "object-lease-controller.ullberg.io/lease-status"

	// Cleanup job annotation keys
	AnnOnDeleteJob       = "object-lease-controller.ullberg.io/on-delete-job"
	AnnJobServiceAccount = "object-lease-controller.ullberg.io/job-service-account"
	AnnJobImage          = "object-lease-controller.ullberg.io/job-image"
	AnnJobWait           = "object-lease-controller.ullberg.io/job-wait"
	AnnJobTimeout        = "object-lease-controller.ullberg.io/job-timeout"
	AnnJobTTL            = "object-lease-controller.ullberg.io/job-ttl"
	AnnJobBackoffLimit   = "object-lease-controller.ullberg.io/job-backoff-limit"
	AnnJobEnvSecrets     = "object-lease-controller.ullberg.io/job-env-secrets"
)

// ParseParams holds runtime configuration parsed from flags and environment.
type ParseParams struct {
	Group                   string
	Version                 string
	Kind                    string
	OptInLabelKey           string
	OptInLabelValue         string
	MetricsBindAddress      string
	HealthProbeBindAddress  string
	PprofBindAddress        string
	LeaderElectionEnabled   bool
	LeaderElectionNamespace string
}

var (
	setupLog = ctrl.Log.WithName("setup")
)

// Allow injection for testing
var exitFn = os.Exit

// Allow injection of controller manager constructor in tests
var newManager = ctrl.NewManager

// Allow injection for testing
var statFn = os.Stat
var readFileFn = os.ReadFile

func main() {
	// Bind zap logging flags (e.g., -zap-log-level) to the global flag set
	// so callers (and the Makefile) can adjust verbosity. Don't set the
	// logger until after flags are parsed so the selected level is applied.
	var zapOpts zap.Options
	zapOpts.BindFlags(flag.CommandLine)

	params := parseParameters()

	// Set logger using the parsed zap options (this reads values parsed by
	// parseParameters which calls flag.Parse()). This allows callers to pass
	// flags like -zap-log-level=debug to control verbosity.
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zapOpts)))

	run(params)
}

// run implements the main logic separately so tests can call it directly.
func run(params ParseParams) {
	enableLeaderElection, leaderElectionNamespace, errE := parseLeaderElectionConfig(params.LeaderElectionEnabled, params.LeaderElectionNamespace)
	if errE != nil {
		fmt.Printf("%v\n", errE)
		exitFn(1)
		return
	}

	if params.Version == "" || params.Kind == "" {
		fmt.Println("Usage: lease-controller -group=GROUP -version=VERSION -kind=KIND [--leader-elect] [--leader-elect-namespace=NAMESPACE]")
		fmt.Println("Or set LEASE_GVK_GROUP, LEASE_GVK_VERSION, LEASE_GVK_KIND, LEASE_LEADER_ELECTION env vars")
		exitFn(1)
		return
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)

	gvk := schema.GroupVersionKind{
		Group:   params.Group,
		Version: params.Version,
		Kind:    params.Kind,
	}

	// Use a unique leader election ID per GVK in lower case
	leaderElectionID := strings.ToLower(fmt.Sprintf("object-lease-controller-%s-%s-%s", params.Group, params.Version, params.Kind))

	mgrOpts := buildManagerOptions(scheme, params.Group, params.Version, params.Kind, params.MetricsBindAddress, params.HealthProbeBindAddress, params.PprofBindAddress, enableLeaderElection, leaderElectionNamespace)

	mgr, err := newManager(ctrl.GetConfigOrDie(), mgrOpts)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		panic(err)
	}

	// Create a LeaseWatcher for the specified GVK
	lw := newLeaseWatcher(mgr, gvk, leaderElectionID)

	if tr, err := configureNamespaceReconciler(mgr, params.OptInLabelKey, params.OptInLabelValue, leaderElectionID); err != nil {
		setupLog.Error(err, "unable to create controller", "GVK", gvk)
		panic(err)
	} else {
		lw.Tracker = tr
	}

	// Register the LeaseWatcher with the manager
	if err := lw.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "GVK", gvk)
		panic(err)
	}

	// Add metrics server expvar handler
	if params.MetricsBindAddress != "" {
		setupLog.Info("Adding /debug/vars to metrics", "address", params.MetricsBindAddress)
		if err := mgr.AddMetricsServerExtraHandler("/debug/vars", expvar.Handler()); err != nil {
			setupLog.Error(err, "unable to set up metrics server extra handler")
			exitFn(1)
			return
		}
	}

	healthCheck := func(req *http.Request) error {
		return healthCheck(req, mgr, gvk)
	}

	if err := mgr.AddHealthzCheck("gvk", healthCheck); err != nil {
		setupLog.Error(err, "unable to set up health check")
		exitFn(1)
		return
	}

	// Ready check: verify manager cache is synced
	readyCheck := newReadyCheck(mgr)
	if err := mgr.AddReadyzCheck("readyz", readyCheck); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		exitFn(1)
		return
	}

	setupLog.Info("Starting manager", "group", params.Group, "version", params.Version, "kind", params.Kind, "leaderElectionID", leaderElectionID)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		panic(err)
	}
}

// newReadyCheck returns a callback that verifies the manager cache is synced.
func newReadyCheck(mgr ctrl.Manager) func(req *http.Request) error {
	return func(req *http.Request) error {
		if !mgr.GetCache().WaitForCacheSync(req.Context()) {
			return fmt.Errorf("cache not synced")
		}
		return nil
	}
}

// parseParameters returns a ParseParams struct instead of a tuple to make it
// easier to extend and pass around in tests.
func parseParameters() ParseParams {
	var group, version, kind string
	var optInLabelKey, optInLabelValue string
	flag.StringVar(&group, "group", "", "Kubernetes API group (e.g., \"apps\")")
	flag.StringVar(&version, "version", "", "Kubernetes API version (e.g., \"v1\")")
	flag.StringVar(&kind, "kind", "", "Kubernetes Kind (e.g., \"ConfigMap\")")

	flag.StringVar(&optInLabelKey, "opt-in-label-key", "", "The label key to opt-in namespaces")
	flag.StringVar(&optInLabelValue, "opt-in-label-value", "", "The label value to opt-in namespaces")

	var metricsAddr, probeAddr, pprofAddr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&pprofAddr, "pprof-bind-address", ":6060", "pprof address")

	var enableLeaderElection bool
	var leaderElectionNamespace string
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")

	flag.StringVar(&leaderElectionNamespace, "leader-elect-namespace", "",
		"Namespace for leader election lock. Defaults to the namespace of the controller manager.")

	flag.Parse()

	// Allow env vars as fallback
	if group == "" {
		group = os.Getenv("LEASE_GVK_GROUP")
	}
	if version == "" {
		version = os.Getenv("LEASE_GVK_VERSION")
	}
	if kind == "" {
		kind = os.Getenv("LEASE_GVK_KIND")
	}
	if optInLabelKey == "" {
		optInLabelKey = os.Getenv("LEASE_OPT_IN_LABEL_KEY")
	}
	if optInLabelValue == "" {
		optInLabelValue = os.Getenv("LEASE_OPT_IN_LABEL_VALUE")
	}

	// Leader election may be enabled via env var when not set via flags
	if !enableLeaderElection {
		if ele := os.Getenv("LEASE_LEADER_ELECTION"); ele != "" {
			if strings.EqualFold(ele, "true") || ele == "1" {
				enableLeaderElection = true
			}
		}
	}

	// If no leader election namespace was provided via flags, allow env fallback.
	if leaderElectionNamespace == "" {
		leaderElectionNamespace = os.Getenv("LEASE_LEADER_ELECTION_NAMESPACE")
	}

	return ParseParams{
		Group:                   group,
		Version:                 version,
		Kind:                    kind,
		OptInLabelKey:           optInLabelKey,
		OptInLabelValue:         optInLabelValue,
		MetricsBindAddress:      metricsAddr,
		HealthProbeBindAddress:  probeAddr,
		PprofBindAddress:        pprofAddr,
		LeaderElectionEnabled:   enableLeaderElection,
		LeaderElectionNamespace: leaderElectionNamespace,
	}
}

// Parse leader election configuration from flags and environment.
// Returns (enabled, namespace, error)
func parseLeaderElectionConfig(enableLeaderElection bool, leaderElectionNamespace string) (bool, string, error) {
	if !enableLeaderElection {
		if val := os.Getenv("LEASE_LEADER_ELECTION"); val != "" {
			var err error
			enableLeaderElection, err = strconv.ParseBool(val)
			if err != nil {
				return false, "", fmt.Errorf("invalid LEASE_LEADER_ELECTION value: %v", err)
			}

			if enableLeaderElection && leaderElectionNamespace == "" {
				leaderElectionNamespace = os.Getenv("LEASE_LEADER_ELECTION_NAMESPACE")
			}

			if enableLeaderElection && leaderElectionNamespace == "" {
				if _, err := statFn("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); os.IsNotExist(err) {
					return false, "", fmt.Errorf("leader election enabled but LEASE_LEADER_ELECTION_NAMESPACE is not set; set it to a valid namespace")
				}
				// we're in a cluster; default to serviceaccount namespace
				if data, err := readFileFn("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
					leaderElectionNamespace = strings.TrimSpace(string(data))
				}
			}
		}
	}

	if leaderElectionNamespace == "" {
		leaderElectionNamespace = os.Getenv("LEASE_LEADER_ELECTION_NAMESPACE")
	}
	return enableLeaderElection, leaderElectionNamespace, nil
}

// Build manager options for a given GVK and flags; extracted for unit testing
func buildManagerOptions(scheme *runtime.Scheme, group, version, kind string, metricsAddr, probeAddr, pprofAddr string, enableLeaderElection bool, leaderElectionNamespace string) ctrl.Options {
	leaderElectionID := strings.ToLower(fmt.Sprintf("object-lease-controller-%s-%s-%s", group, version, kind))
	metricsServerOptions := metricsserver.Options{BindAddress: metricsAddr}
	mgrOpts := ctrl.Options{
		Scheme:                        scheme,
		LeaderElection:                enableLeaderElection,
		LeaderElectionID:              leaderElectionID,
		LeaderElectionNamespace:       leaderElectionNamespace,
		LeaderElectionReleaseOnCancel: true,
		Metrics:                       metricsServerOptions,
		HealthProbeBindAddress:        probeAddr,
		Cache: cache.Options{
			DefaultTransform: util.MinimalObjectTransform(
				AnnTTL, AnnLeaseStart, AnnExpireAt, AnnStatus,
				AnnOnDeleteJob, AnnJobServiceAccount, AnnJobImage, AnnJobWait,
				AnnJobTimeout, AnnJobTTL, AnnJobBackoffLimit,
			),
		},
	}
	if pprofAddr != "" {
		mgrOpts.PprofBindAddress = pprofAddr
	}
	return mgrOpts
}

// Create a LeaseWatcher attached to the given manager. The LeaseWatcher is initialized
// with default annotations and metrics for the provided GVK. The function does not
// call SetupWithManager - this is left to the caller.
func newLeaseWatcher(mgr ctrl.Manager, gvk schema.GroupVersionKind, leaderElectionID string) *controllers.LeaseWatcher {
	return &controllers.LeaseWatcher{
		Client:   mgr.GetClient(),
		GVK:      gvk,
		Recorder: mgr.GetEventRecorderFor(leaderElectionID),
		Annotations: controllers.Annotations{
			TTL:               AnnTTL,
			LeaseStart:        AnnLeaseStart,
			ExpireAt:          AnnExpireAt,
			Status:            AnnStatus,
			OnDeleteJob:       AnnOnDeleteJob,
			JobServiceAccount: AnnJobServiceAccount,
			JobImage:          AnnJobImage,
			JobWait:           AnnJobWait,
			JobTimeout:        AnnJobTimeout,
			JobTTL:            AnnJobTTL,
			JobBackoffLimit:   AnnJobBackoffLimit,
			JobEnvSecrets:     AnnJobEnvSecrets,
		},
		Metrics: ometrics.NewLeaseMetrics(gvk),
	}
}

// If optInLabelKey and optInLabelValue are provided, create a NamespaceReconciler and
// register it with the manager. Returns the NamespaceTracker that was created if any,
// or nil if opt-in was not requested.
func configureNamespaceReconciler(mgr ctrl.Manager, optInLabelKey, optInLabelValue, leaderElectionID string) (*util.NamespaceTracker, error) {
	if optInLabelKey == "" || optInLabelValue == "" {
		return nil, nil
	}
	tracker := util.NewNamespaceTracker()
	nw := &controllers.NamespaceReconciler{
		Client:     mgr.GetClient(),
		Recorder:   mgr.GetEventRecorderFor(leaderElectionID),
		LabelKey:   optInLabelKey,
		LabelValue: optInLabelValue,
		Tracker:    tracker,
	}
	if err := nw.SetupWithManager(mgr); err != nil {
		return nil, err
	}
	return tracker, nil
}

// Health check: confirm GVK is discoverable and listable with minimal load
func healthCheck(req *http.Request, mgr ctrl.Manager, gvk schema.GroupVersionKind) error {
	ctx := req.Context()

	// Resolve scope from RESTMapper
	mapping, err := mgr.GetRESTMapper().RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("rest mapping failed for %s: %w", gvk.String(), err)
	}
	namespaced := mapping.Scope.Name() == apimeta.RESTScopeNameNamespace

	// Build an unstructured list for the configured GVK
	ul := &unstructured.UnstructuredList{}
	ul.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind + "List",
	})

	// Cheap probe: limit to 1 item and namespace only if namespaced
	var opts []client.ListOption
	opts = append(opts, client.Limit(1))
	if namespaced {
		ns := "default"
		if nsEnv := os.Getenv("LEASE_NAMESPACE"); nsEnv != "" {
			ns = nsEnv
		}
		opts = append(opts, client.InNamespace(ns))
	}

	if err := mgr.GetAPIReader().List(ctx, ul, opts...); err != nil {
		return fmt.Errorf("list probe failed for %s: %w", gvk.String(), err)
	}
	return nil
}
