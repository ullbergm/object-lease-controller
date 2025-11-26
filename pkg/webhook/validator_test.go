package webhook

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

const validatorTTLAnnotation = "object-lease-controller.ullberg.io/ttl"

// mockConfigProvider implements ConfigProvider for testing
type mockConfigProvider struct {
	validGVKs map[schema.GroupVersionKind]bool
}

func (m *mockConfigProvider) ShouldValidate(gvk schema.GroupVersionKind) bool {
	return m.validGVKs[gvk]
}

func newMockProvider(gvks ...schema.GroupVersionKind) *mockConfigProvider {
	p := &mockConfigProvider{
		validGVKs: make(map[schema.GroupVersionKind]bool),
	}
	for _, gvk := range gvks {
		p.validGVKs[gvk] = true
	}
	return p
}

func makeAdmissionReview(gvk schema.GroupVersionKind, rawObject []byte) *admissionv1.AdmissionReview {
	return &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Request: &admissionv1.AdmissionRequest{
			UID: types.UID("test-uid"),
			Kind: metav1.GroupVersionKind{
				Group:   gvk.Group,
				Version: gvk.Version,
				Kind:    gvk.Kind,
			},
			Namespace: "default",
			Name:      "test-object",
			Object: runtime.RawExtension{
				Raw: rawObject,
			},
		},
	}
}

func TestNewDynamicValidator(t *testing.T) {
	provider := newMockProvider()
	v := NewDynamicValidator(validatorTTLAnnotation, provider)

	if v == nil {
		t.Fatal("expected non-nil validator")
	}
	if v.TTLAnnotation != validatorTTLAnnotation {
		t.Errorf("TTLAnnotation = %q, want %q", v.TTLAnnotation, validatorTTLAnnotation)
	}
	if v.ConfigProvider != provider {
		t.Error("ConfigProvider mismatch")
	}
}

func TestHandle_MethodNotAllowed(t *testing.T) {
	provider := newMockProvider()
	v := NewDynamicValidator(validatorTTLAnnotation, provider)

	req := httptest.NewRequest(http.MethodGet, "/validate", nil)
	w := httptest.NewRecorder()

	v.Handle(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, w.Code)
	}
}

func TestHandle_InvalidBody(t *testing.T) {
	provider := newMockProvider()
	v := NewDynamicValidator(validatorTTLAnnotation, provider)

	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader([]byte("invalid json")))
	w := httptest.NewRecorder()

	v.Handle(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandle_NilRequest(t *testing.T) {
	provider := newMockProvider()
	v := NewDynamicValidator(validatorTTLAnnotation, provider)

	review := &admissionv1.AdmissionReview{
		Request: nil,
	}
	body, _ := json.Marshal(review)

	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	v.Handle(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandle_GVKNotConfigured(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	provider := newMockProvider() // No GVKs configured

	v := NewDynamicValidator(validatorTTLAnnotation, provider)

	rawObject := []byte(`{"metadata": {"name": "test", "annotations": {"object-lease-controller.ullberg.io/ttl": "invalid"}}}`)
	review := makeAdmissionReview(gvk, rawObject)
	body, _ := json.Marshal(review)

	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	v.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response admissionv1.AdmissionReview
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !response.Response.Allowed {
		t.Error("expected request to be allowed for unconfigured GVK")
	}
}

func TestHandle_ValidTTL(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	provider := newMockProvider(gvk)

	v := NewDynamicValidator(validatorTTLAnnotation, provider)

	rawObject := []byte(`{"metadata": {"name": "test", "annotations": {"object-lease-controller.ullberg.io/ttl": "1h30m"}}}`)
	review := makeAdmissionReview(gvk, rawObject)
	body, _ := json.Marshal(review)

	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	v.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response admissionv1.AdmissionReview
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !response.Response.Allowed {
		t.Error("expected request to be allowed for valid TTL")
	}
}

func TestHandle_InvalidTTL(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	provider := newMockProvider(gvk)

	v := NewDynamicValidator(validatorTTLAnnotation, provider)

	rawObject := []byte(`{"metadata": {"name": "test", "annotations": {"object-lease-controller.ullberg.io/ttl": "invalid-ttl"}}}`)
	review := makeAdmissionReview(gvk, rawObject)
	body, _ := json.Marshal(review)

	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	v.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response admissionv1.AdmissionReview
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Response.Allowed {
		t.Error("expected request to be denied for invalid TTL")
	}
	if response.Response.Result == nil {
		t.Fatal("expected result to be set")
	}
	if response.Response.Result.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected code %d, got %d", http.StatusUnprocessableEntity, response.Response.Result.Code)
	}
}

func TestHandle_NoTTLAnnotation(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	provider := newMockProvider(gvk)

	v := NewDynamicValidator(validatorTTLAnnotation, provider)

	rawObject := []byte(`{"metadata": {"name": "test", "annotations": {"other-annotation": "value"}}}`)
	review := makeAdmissionReview(gvk, rawObject)
	body, _ := json.Marshal(review)

	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	v.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response admissionv1.AdmissionReview
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !response.Response.Allowed {
		t.Error("expected request to be allowed when no TTL annotation present")
	}
}

func TestHandle_NoAnnotations(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	provider := newMockProvider(gvk)

	v := NewDynamicValidator(validatorTTLAnnotation, provider)

	rawObject := []byte(`{"metadata": {"name": "test"}}`)
	review := makeAdmissionReview(gvk, rawObject)
	body, _ := json.Marshal(review)

	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	v.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response admissionv1.AdmissionReview
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !response.Response.Allowed {
		t.Error("expected request to be allowed when no annotations present")
	}
}

func TestHandle_NoMetadata(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	provider := newMockProvider(gvk)

	v := NewDynamicValidator(validatorTTLAnnotation, provider)

	rawObject := []byte(`{"spec": {"replicas": 1}}`)
	review := makeAdmissionReview(gvk, rawObject)
	body, _ := json.Marshal(review)

	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	v.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response admissionv1.AdmissionReview
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !response.Response.Allowed {
		t.Error("expected request to be allowed when no metadata present")
	}
}

func TestHandle_MalformedRawObject(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	provider := newMockProvider(gvk)

	v := NewDynamicValidator(validatorTTLAnnotation, provider)

	// Create an object with malformed metadata (annotations is not a map)
	rawObject := []byte(`{"metadata": {"name": "test", "annotations": "not-a-map"}}`)
	review := makeAdmissionReview(gvk, rawObject)
	body, _ := json.Marshal(review)

	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	v.Handle(w, req)

	// Should return 200 OK and allow (since annotations is malformed, TTL annotation doesn't exist)
	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response admissionv1.AdmissionReview
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// When annotations is not a map, the code treats it as "no annotations"
	// and allows the request
	if !response.Response.Allowed {
		t.Error("expected request to be allowed when annotations is malformed (no TTL)")
	}
}

func TestHandle_TTLNotString(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	provider := newMockProvider(gvk)

	v := NewDynamicValidator(validatorTTLAnnotation, provider)

	// TTL is a number instead of string
	rawObject := []byte(`{"metadata": {"name": "test", "annotations": {"object-lease-controller.ullberg.io/ttl": 123}}}`)
	review := makeAdmissionReview(gvk, rawObject)
	body, _ := json.Marshal(review)

	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	v.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response admissionv1.AdmissionReview
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Response.Allowed {
		t.Error("expected request to be denied when TTL is not a string")
	}
	if response.Response.Result == nil || response.Response.Result.Code != http.StatusUnprocessableEntity {
		t.Error("expected UnprocessableEntity status")
	}
}

func TestHandle_VariousTTLFormats(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	provider := newMockProvider(gvk)

	v := NewDynamicValidator(validatorTTLAnnotation, provider)

	validTTLs := []string{
		"1h",
		"30m",
		"2d",
		"1w",
		"1h30m",
		"2d3h",
		"1w2d3h",
		"30s",
		"1.5h",
	}

	for _, ttl := range validTTLs {
		t.Run("valid_"+ttl, func(t *testing.T) {
			rawObject := []byte(`{"metadata": {"name": "test", "annotations": {"object-lease-controller.ullberg.io/ttl": "` + ttl + `"}}}`)
			review := makeAdmissionReview(gvk, rawObject)
			body, _ := json.Marshal(review)

			req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			v.Handle(w, req)

			var response admissionv1.AdmissionReview
			if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if !response.Response.Allowed {
				t.Errorf("expected TTL %q to be valid", ttl)
			}
		})
	}

	invalidTTLs := []string{
		"abc",
		"1x",
		"forever",
		"",
	}

	for _, ttl := range invalidTTLs {
		t.Run("invalid_"+ttl, func(t *testing.T) {
			rawObject := []byte(`{"metadata": {"name": "test", "annotations": {"object-lease-controller.ullberg.io/ttl": "` + ttl + `"}}}`)
			review := makeAdmissionReview(gvk, rawObject)
			body, _ := json.Marshal(review)

			req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			v.Handle(w, req)

			var response admissionv1.AdmissionReview
			if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if response.Response.Allowed {
				t.Errorf("expected TTL %q to be invalid", ttl)
			}
		})
	}
}

func TestHandle_ResponseUID(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	provider := newMockProvider(gvk)

	v := NewDynamicValidator(validatorTTLAnnotation, provider)

	rawObject := []byte(`{"metadata": {"name": "test"}}`)
	review := makeAdmissionReview(gvk, rawObject)
	review.Request.UID = "unique-request-id"
	body, _ := json.Marshal(review)

	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	v.Handle(w, req)

	var response admissionv1.AdmissionReview
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Response.UID != "unique-request-id" {
		t.Errorf("expected UID to be preserved, got %q", response.Response.UID)
	}
}

func TestHandle_CoreGroup(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	provider := newMockProvider(gvk)

	v := NewDynamicValidator(validatorTTLAnnotation, provider)

	rawObject := []byte(`{"metadata": {"name": "test", "annotations": {"object-lease-controller.ullberg.io/ttl": "1h"}}}`)
	review := makeAdmissionReview(gvk, rawObject)
	body, _ := json.Marshal(review)

	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	v.Handle(w, req)

	var response admissionv1.AdmissionReview
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !response.Response.Allowed {
		t.Error("expected request to be allowed for core group resources")
	}
}

func TestHandle_ReadBodyError(t *testing.T) {
	provider := newMockProvider()
	v := NewDynamicValidator(validatorTTLAnnotation, provider)

	// Create a request with a body that fails on read
	req := httptest.NewRequest(http.MethodPost, "/validate", &errorReader{})
	w := httptest.NewRecorder()

	v.Handle(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

// errorReader is an io.Reader that always returns an error
type errorReader struct{}

func (e *errorReader) Read(p []byte) (n int, err error) {
	return 0, io.ErrUnexpectedEOF
}

func TestValidate_DirectCall(t *testing.T) {
	provider := newMockProvider()
	v := NewDynamicValidator(validatorTTLAnnotation, provider)

	// Test validate directly with a valid object
	rawObject := []byte(`{"metadata": {"name": "test", "annotations": {"object-lease-controller.ullberg.io/ttl": "1h"}}}`)
	req := &admissionv1.AdmissionRequest{
		UID:       "test-uid",
		Namespace: "default",
		Name:      "test-obj",
		Object: runtime.RawExtension{
			Raw: rawObject,
		},
	}

	response := v.validate(req)
	if !response.Allowed {
		t.Error("expected valid TTL to be allowed")
	}
}
