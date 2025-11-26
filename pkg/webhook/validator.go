package webhook

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	ctrl "sigs.k8s.io/controller-runtime"

	"object-lease-controller/pkg/util"
)

var (
	log    = ctrl.Log.WithName("webhook")
	scheme = runtime.NewScheme()
	codecs = serializer.NewCodecFactory(scheme)
)

func init() {
	_ = admissionv1.AddToScheme(scheme)
}

// ConfigProvider provides GVK-specific webhook configuration
type ConfigProvider interface {
	ShouldValidate(gvk schema.GroupVersionKind) bool
}

// DynamicValidator validates lease annotations with dynamic GVK configuration
type DynamicValidator struct {
	TTLAnnotation  string
	ConfigProvider ConfigProvider
}

// NewDynamicValidator creates a new validator with dynamic configuration
func NewDynamicValidator(ttlAnnotation string, provider ConfigProvider) *DynamicValidator {
	return &DynamicValidator{
		TTLAnnotation:  ttlAnnotation,
		ConfigProvider: provider,
	}
}

// Handle processes admission requests and validates lease annotations
func (v *DynamicValidator) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Error(err, "failed to read request body")
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	defer func() {
		_ = r.Body.Close()
	}()

	// Decode the admission review request
	admissionReview := &admissionv1.AdmissionReview{}
	deserializer := codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(body, nil, admissionReview); err != nil {
		log.Error(err, "failed to decode admission review")
		http.Error(w, "failed to decode admission review", http.StatusBadRequest)
		return
	}

	if admissionReview.Request == nil {
		log.Error(fmt.Errorf("admission review request is nil"), "invalid admission review")
		http.Error(w, "invalid admission review", http.StatusBadRequest)
		return
	}

	// Check if we should validate this GVK
	requestGVK := schema.GroupVersionKind{
		Group:   admissionReview.Request.Kind.Group,
		Version: admissionReview.Request.Kind.Version,
		Kind:    admissionReview.Request.Kind.Kind,
	}

	if !v.ConfigProvider.ShouldValidate(requestGVK) {
		// GVK not configured for validation, allow
		response := &admissionv1.AdmissionReview{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "admission.k8s.io/v1",
				Kind:       "AdmissionReview",
			},
			Response: &admissionv1.AdmissionResponse{
				UID:     admissionReview.Request.UID,
				Allowed: true,
			},
		}
		v.writeResponse(w, response)
		return
	}

	// Validate the object
	validationResponse := v.validate(admissionReview.Request)

	// Construct the admission review response
	reviewResponse := &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Response: validationResponse,
	}
	reviewResponse.Response.UID = admissionReview.Request.UID

	v.writeResponse(w, reviewResponse)
}

func (v *DynamicValidator) writeResponse(w http.ResponseWriter, response *admissionv1.AdmissionReview) {
	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	if err := encoder.Encode(response); err != nil {
		log.Error(err, "failed to encode admission response")
		return
	}
}

// validate checks if the TTL annotation has a valid format
func (v *DynamicValidator) validate(req *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	// Parse the object from the request
	obj := make(map[string]interface{})
	if err := json.Unmarshal(req.Object.Raw, &obj); err != nil {
		log.Error(err, "failed to unmarshal object", "namespace", req.Namespace, "name", req.Name)
		return &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Status:  metav1.StatusFailure,
				Message: fmt.Sprintf("failed to parse object: %v", err),
				Code:    http.StatusBadRequest,
			},
		}
	}

	// Extract annotations
	metadata, ok := obj["metadata"].(map[string]interface{})
	if !ok {
		// No metadata, allow the request (no annotations to validate)
		return &admissionv1.AdmissionResponse{
			Allowed: true,
		}
	}

	annMap, ok := metadata["annotations"].(map[string]interface{})
	if !ok {
		// No annotations, allow the request
		return &admissionv1.AdmissionResponse{
			Allowed: true,
		}
	}

	// Check if the TTL annotation exists
	ttlValue, exists := annMap[v.TTLAnnotation]
	if !exists {
		// No TTL annotation, allow the request
		return &admissionv1.AdmissionResponse{
			Allowed: true,
		}
	}

	// Validate the TTL value format
	ttlStr, ok := ttlValue.(string)
	if !ok {
		return &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Status:  metav1.StatusFailure,
				Message: fmt.Sprintf("annotation %q must be a string", v.TTLAnnotation),
				Code:    http.StatusUnprocessableEntity,
			},
		}
	}

	// Use the existing ParseFlexibleDuration function to validate
	if _, err := util.ParseFlexibleDuration(ttlStr); err != nil {
		return &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Status: metav1.StatusFailure,
				Message: fmt.Sprintf("invalid TTL format in annotation %q: %v. "+
					"Valid formats: combinations of weeks (w), days (d), hours (h), minutes (m), seconds (s). "+
					"Examples: '2d', '1h30m', '1w', '30s'",
					v.TTLAnnotation, err),
				Code: http.StatusUnprocessableEntity,
			},
		}
	}

	// TTL is valid, allow the request
	log.V(1).Info("validated TTL annotation",
		"namespace", req.Namespace,
		"name", req.Name,
		"kind", req.Kind.Kind,
		"ttl", ttlStr)

	return &admissionv1.AdmissionResponse{
		Allowed: true,
	}
}
