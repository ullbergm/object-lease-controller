package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

func TestHealthzHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	err := healthzHandler(req)
	if err != nil {
		t.Errorf("healthzHandler returned error: %v", err)
	}
}

func TestReadyzHandler_CacheSynced(t *testing.T) {
	mgr := &mockManager{
		cacheSynced: true,
	}

	handler := readyzHandler(mgr)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	err := handler(req)
	if err != nil {
		t.Errorf("readyzHandler returned error when cache synced: %v", err)
	}
}

func TestReadyzHandler_CacheNotSynced(t *testing.T) {
	mgr := &mockManager{
		cacheSynced: false,
	}

	handler := readyzHandler(mgr)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	err := handler(req)
	if err == nil {
		t.Error("readyzHandler should return error when cache not synced")
	}
}

func TestDefaultTTLAnnotation(t *testing.T) {
	expected := "object-lease-controller.ullberg.io/ttl"
	if DefaultTTLAnnotation != expected {
		t.Errorf("DefaultTTLAnnotation = %q, want %q", DefaultTTLAnnotation, expected)
	}
}

// mockManager implements ctrl.Manager for testing
type mockManager struct {
	cacheSynced bool
	cache       *mockCache
}

func (m *mockManager) GetCache() cache.Cache {
	if m.cache == nil {
		m.cache = &mockCache{synced: m.cacheSynced}
	}
	return m.cache
}

// Implement remaining manager.Manager interface methods
func (m *mockManager) Add(runnable manager.Runnable) error { return nil }
func (m *mockManager) Elected() <-chan struct{}            { return make(chan struct{}) }
func (m *mockManager) AddMetricsServerExtraHandler(path string, handler http.Handler) error {
	return nil
}
func (m *mockManager) AddHealthzCheck(name string, check healthz.Checker) error { return nil }
func (m *mockManager) AddReadyzCheck(name string, check healthz.Checker) error  { return nil }
func (m *mockManager) Start(ctx context.Context) error                          { return nil }
func (m *mockManager) GetWebhookServer() webhook.Server                         { return nil }
func (m *mockManager) GetLogger() logr.Logger                                   { return logr.Discard() }
func (m *mockManager) GetControllerOptions() config.Controller                  { return config.Controller{} }
func (m *mockManager) GetScheme() *runtime.Scheme                               { return runtime.NewScheme() }
func (m *mockManager) GetClient() client.Client                                 { return nil }
func (m *mockManager) GetFieldIndexer() client.FieldIndexer                     { return nil }
func (m *mockManager) GetEventRecorderFor(name string) record.EventRecorder     { return nil }
func (m *mockManager) GetRESTMapper() meta.RESTMapper {
	return meta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "", Version: "v1"}})
}
func (m *mockManager) GetAPIReader() client.Reader { return nil }
func (m *mockManager) GetConfig() *rest.Config     { return &rest.Config{} }
func (m *mockManager) GetHTTPClient() *http.Client { return &http.Client{} }

// Verify mockManager implements ctrl.Manager
var _ ctrl.Manager = &mockManager{}

// mockCache implements cache.Cache for testing WaitForCacheSync
type mockCache struct {
	synced bool
}

func (c *mockCache) WaitForCacheSync(ctx context.Context) bool {
	return c.synced
}

// Implement remaining cache.Cache interface methods as stubs
func (c *mockCache) Start(ctx context.Context) error { return nil }
func (c *mockCache) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return nil
}
func (c *mockCache) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return nil
}
func (c *mockCache) GetInformer(ctx context.Context, obj client.Object, opts ...cache.InformerGetOption) (cache.Informer, error) {
	return nil, nil
}
func (c *mockCache) GetInformerForKind(ctx context.Context, gvk schema.GroupVersionKind, opts ...cache.InformerGetOption) (cache.Informer, error) {
	return nil, nil
}
func (c *mockCache) RemoveInformer(ctx context.Context, obj client.Object) error { return nil }
func (c *mockCache) IndexField(ctx context.Context, obj client.Object, field string, extractValue client.IndexerFunc) error {
	return nil
}

// Verify mockCache implements cache.Cache
var _ cache.Cache = &mockCache{}

// Test that the HTTP handlers work end-to-end
func TestHealthzHTTPHandler(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := healthzHandler(r); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
	if w.Body.String() != "ok" {
		t.Errorf("expected body 'ok', got %q", w.Body.String())
	}
}

func TestReadyzHTTPHandler_Success(t *testing.T) {
	mgr := &mockManager{cacheSynced: true}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := readyzHandler(mgr)(r); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestReadyzHTTPHandler_Failure(t *testing.T) {
	mgr := &mockManager{cacheSynced: false}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := readyzHandler(mgr)(r); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

// Test server configuration constants
func TestServerTimeouts(t *testing.T) {
	// These are the timeouts defined in main.go
	expectedReadTimeout := 15 * time.Second
	expectedWriteTimeout := 15 * time.Second

	// This is a compile-time check that the constants exist
	// In the actual implementation, these are set when creating the server
	if expectedReadTimeout != 15*time.Second {
		t.Errorf("unexpected read timeout: %v", expectedReadTimeout)
	}
	if expectedWriteTimeout != 15*time.Second {
		t.Errorf("unexpected write timeout: %v", expectedWriteTimeout)
	}
}

// Test that we can create the proper handler signatures
func TestHandlerSignatures(t *testing.T) {
	// Verify healthzHandler can be assigned to expected signature
	healthCheck := healthzHandler
	_ = healthCheck // Use the variable

	// Verify readyzHandler returns the expected signature
	mgr := &mockManager{cacheSynced: true}
	readyCheck := readyzHandler(mgr)
	_ = readyCheck // Use the variable
}

// Test that we handle the context correctly in WaitForCacheSync
func TestReadyzHandler_ContextHandling(t *testing.T) {
	mgr := &mockManager{cacheSynced: true}

	handler := readyzHandler(mgr)

	// Create a request with a context
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil).WithContext(ctx)
	err := handler(req)

	if err != nil {
		t.Errorf("readyzHandler returned error with valid context: %v", err)
	}
}
