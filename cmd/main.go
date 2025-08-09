package main

import (
	"expvar"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	controllers "object-lease-controller/pkg/controllers"
	"object-lease-controller/pkg/util"
)

var (
	setupLog = ctrl.Log.WithName("setup")
)

func main() {
	ctrl.SetLogger(zap.New())

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

	if !enableLeaderElection {
		if val := os.Getenv("LEASE_LEADER_ELECTION"); val != "" {
			var err error
			enableLeaderElection, err = strconv.ParseBool(val)
			if err != nil {
				fmt.Printf("Invalid LEASE_LEADER_ELECTION value: %v\n", err)
				os.Exit(1)
			}

			// If leader election is enabled, check for namespace and fail if not set and not running in a cluster
			if enableLeaderElection && leaderElectionNamespace == "" {
				leaderElectionNamespace = os.Getenv("LEASE_LEADER_ELECTION_NAMESPACE")
			}
			if leaderElectionNamespace == "" {
				// If running outside a cluster, we need a namespace for leader election
				if _, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); os.IsNotExist(err) {
					fmt.Println("Leader election enabled but LEASE_LEADER_ELECTION_NAMESPACE is not set. Please set it to a valid namespace.")
					os.Exit(1)
				} else {
					// Default to the namespace file if running in a cluster
					data, _ := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
					leaderElectionNamespace = strings.TrimSpace(string(data))
				}
			}
		}
	}

	if version == "" || kind == "" {
		fmt.Println("Usage: lease-manager -group=GROUP -version=VERSION -kind=KIND [--leader-elect] [--leader-elect-namespace=NAMESPACE]")
		fmt.Println("Or set LEASE_GVK_GROUP, LEASE_GVK_VERSION, LEASE_GVK_KIND, LEASE_LEADER_ELECTION env vars")
		os.Exit(1)
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	gvk := schema.GroupVersionKind{
		Group:   group,
		Version: version,
		Kind:    kind,
	}

	// Use a unique leader election ID per GVK in lower case
	leaderElectionID := strings.ToLower(fmt.Sprintf("object-lease-controller-%s-%s-%s", group, version, kind))

	// Set up metrics server options
	metricsServerOptions := metricsserver.Options{
		BindAddress: metricsAddr,
	}

	mgrOpts := ctrl.Options{
		Scheme:                        scheme,
		LeaderElection:                enableLeaderElection,
		LeaderElectionID:              leaderElectionID,
		LeaderElectionNamespace:       leaderElectionNamespace,
		LeaderElectionReleaseOnCancel: true,
		Metrics:                       metricsServerOptions,
		HealthProbeBindAddress:        probeAddr,
		Cache: cache.Options{
			DefaultTransform: cache.TransformStripManagedFields(),
		},
	}

	if pprofAddr != "" {
		mgrOpts.PprofBindAddress = pprofAddr
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), mgrOpts)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		panic(err)
	}

	// Create a LeaseWatcher for the specified GVK
	lw := &controllers.LeaseWatcher{
		Client:   mgr.GetClient(),
		GVK:      gvk,
		Recorder: mgr.GetEventRecorderFor(leaderElectionID),
	}

	if optInLabelKey != "" && optInLabelValue != "" {
		tracker := util.NewNamespaceTracker()

		nw := &controllers.NamespaceReconciler{
			Client:     mgr.GetClient(),
			Recorder:   mgr.GetEventRecorderFor(leaderElectionID),
			LabelKey:   optInLabelKey,
			LabelValue: optInLabelValue,
			Tracker:    tracker,
		}

		// Register NamespaceReconciler with the manager
		if err := nw.SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "GVK", gvk)
			panic(err)
		}

		lw.Tracker = tracker
	}

	// Register the LeaseWatcher with the manager
	if err := lw.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "GVK", gvk)
		panic(err)
	}

	// Add metrics server expvar handler
	if metricsAddr != "" {
		setupLog.Info("Adding /debug/vars to metrics", "address", metricsAddr)
		if err := mgr.AddMetricsServerExtraHandler("/debug/vars", expvar.Handler()); err != nil {
			setupLog.Error(err, "unable to set up metrics server extra handler")
			os.Exit(1)
		}
	}

	// Health check: verify we can talk to the Kubernetes API
	healthCheck := func(req *http.Request) error {
		ctx := req.Context()

		// Try to get all objects of the GVK that we are monitoring
		list := &corev1.ConfigMapList{}
		ns := "default" // Default namespace, can be overridden by env var or flag
		if nsEnv := os.Getenv("LEASE_NAMESPACE"); nsEnv != "" {
			ns = nsEnv
		}
		if err := mgr.GetClient().List(ctx, list); err != nil {
			return fmt.Errorf("failed to list %s/%s/%s in namespace %s: %w", group, version, kind, ns, err)
		}

		return nil
	}
	if err := mgr.AddHealthzCheck("healthz", healthCheck); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}

	// Ready check: verify manager cache is synced
	readyCheck := func(req *http.Request) error {
		if !mgr.GetCache().WaitForCacheSync(req.Context()) {
			return fmt.Errorf("cache not synced")
		}
		return nil
	}
	if err := mgr.AddReadyzCheck("readyz", readyCheck); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("Starting manager", "group", group, "version", version, "kind", kind, "leaderElectionID", leaderElectionID)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		panic(err)
	}
}
