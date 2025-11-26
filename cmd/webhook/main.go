package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"object-lease-controller/pkg/webhook"
)

const (
	// Default annotation key for TTL
	DefaultTTLAnnotation = "object-lease-controller.ullberg.io/ttl"
)

var (
	setupLog = ctrl.Log.WithName("webhook-setup")
)

func main() {
	var (
		webhookPort   int
		certDir       string
		certName      string
		keyName       string
		metricsAddr   string
		probeAddr     string
		leaderElect   bool
		leaderElectNs string
		insecure      bool
	)

	// Parse command-line flags
	flag.IntVar(&webhookPort, "webhook-port", 9443, "Port for the webhook server")
	flag.StringVar(&certDir, "cert-dir", "/tmp/k8s-webhook-server/serving-certs", "Directory containing TLS certificates")
	flag.StringVar(&certName, "cert-name", "tls.crt", "Name of the TLS certificate file")
	flag.StringVar(&keyName, "key-name", "tls.key", "Name of the TLS key file")
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "Address for metrics endpoint")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "Address for health probes")
	flag.BoolVar(&leaderElect, "leader-elect", false, "Enable leader election")
	flag.StringVar(&leaderElectNs, "leader-elect-namespace", "", "Namespace for leader election")
	flag.BoolVar(&insecure, "insecure", false, "Run webhook server without TLS (for local testing only)")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// Create manager
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                  runtime.NewScheme(),
		LeaderElection:          leaderElect,
		LeaderElectionID:        "object-lease-webhook-lock",
		LeaderElectionNamespace: leaderElectNs,
		HealthProbeBindAddress:  probeAddr,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	// Add schemes
	if err := corev1.AddToScheme(mgr.GetScheme()); err != nil {
		setupLog.Error(err, "unable to add core/v1 to scheme")
		os.Exit(1)
	}

	// Create webhook configuration manager
	configMgr := webhook.NewConfigManager(
		mgr.GetClient(),
		DefaultTTLAnnotation,
		setupLog,
	)

	// Start watching LeaseController CRs
	if err := configMgr.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to setup config manager")
		os.Exit(1)
	}

	// Create and start the webhook server
	webhookServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", webhookPort),
		TLSConfig:    &tls.Config{MinVersion: tls.VersionTLS12},
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	// Create validator with the config manager
	validator := webhook.NewDynamicValidator(DefaultTTLAnnotation, configMgr)
	http.HandleFunc("/validate", validator.Handle)
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := healthzHandler(r); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	http.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := readyzHandler(mgr)(r); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Start webhook server in a goroutine
	go func() {
		if insecure {
			setupLog.Info("starting webhook server WITHOUT TLS (insecure mode)", "port", webhookPort)
			if err := webhookServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				setupLog.Error(err, "webhook server failed")
				os.Exit(1)
			}
		} else {
			setupLog.Info("starting webhook server with TLS", "port", webhookPort)
			certFile := fmt.Sprintf("%s/%s", certDir, certName)
			keyFile := fmt.Sprintf("%s/%s", certDir, keyName)

			if err := webhookServer.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
				setupLog.Error(err, "webhook server failed")
				os.Exit(1)
			}
		}
	}()

	// Setup health checks
	if err := mgr.AddHealthzCheck("healthz", healthzHandler); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", readyzHandler(mgr)); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := webhookServer.Shutdown(ctx); err != nil {
		setupLog.Error(err, "webhook server shutdown failed")
	}
}

func healthzHandler(r *http.Request) error {
	return nil
}

func readyzHandler(mgr ctrl.Manager) func(*http.Request) error {
	return func(r *http.Request) error {
		if !mgr.GetCache().WaitForCacheSync(r.Context()) {
			return fmt.Errorf("cache not synced")
		}
		return nil
	}
}
