package main

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/davidcollom/gitrepo-csi-driver/pkg/policy"
	"github.com/davidcollom/gitrepo-csi-driver/pkg/volume"
)

type AdmissionReview struct {
	Request *AdmissionRequest `json:"request,omitempty"`
}

type AdmissionRequest struct {
	UID       string          `json:"uid"`
	Namespace string          `json:"namespace"`
	Object    json.RawMessage `json:"object"`
}

type AdmissionResponse struct {
	UID     string `json:"uid,omitempty"`
	Allowed bool   `json:"allowed"`
	Result  struct {
		Message string `json:"message,omitempty"`
	} `json:"status,omitempty"`
}

type AdmissionReviewResponse struct {
	Response *AdmissionResponse `json:"response,omitempty"`
}

type Pod struct {
	Spec struct {
		Volumes []struct {
			CSI *struct {
				Driver           string            `json:"driver"`
				ReadOnly         bool              `json:"readOnly"`
				VolumeAttributes map[string]string `json:"volumeAttributes"`
			} `json:"csi,omitempty"`
		} `json:"volumes"`
	} `json:"spec"`
}

func main() {
	policyPath := getenv("GITCONTENT_POLICY_FILE", "./examples/policies.yaml")
	addr := getenv("GITCONTENT_ADMISSION_ADDR", ":8443")

	policies, err := policy.LoadPolicies(policyPath)
	if err != nil {
		panic(err)
	}
	evaluator := policy.Evaluator{Policies: policies}

	http.HandleFunc("/validate", func(w http.ResponseWriter, r *http.Request) {
		var review AdmissionReview
		if err := json.NewDecoder(r.Body).Decode(&review); err != nil {
			deny(w, "", "invalid admission review")
			return
		}
		if review.Request == nil {
			deny(w, "", "missing request")
			return
		}

		var pod Pod
		if err := json.Unmarshal(review.Request.Object, &pod); err != nil {
			deny(w, review.Request.UID, "invalid pod object")
			return
		}

		for _, v := range pod.Spec.Volumes {
			if v.CSI == nil {
				continue
			}
			if v.CSI.Driver != "gitcontent.csi.example.io" {
				continue
			}
			if !v.CSI.ReadOnly {
				deny(w, review.Request.UID, "git content volume must be readOnly")
				return
			}

			attrs, err := volume.Parse(v.CSI.VolumeAttributes)
			if err != nil {
				deny(w, review.Request.UID, err.Error())
				return
			}

			res, _ := evaluator.Evaluate(policy.Request{
				Namespace:         review.Request.Namespace,
				Repo:              attrs.Repo,
				Revision:          attrs.Revision,
				Path:              attrs.Path,
				Depth:             attrs.Depth,
				Submodules:        attrs.Submodules,
				LFS:               attrs.LFS,
				CredentialProfile: attrs.CredentialProfile,
			}, attrs.Policy)
			if !res.Allowed {
				deny(w, review.Request.UID, res.DenialReason)
				return
			}
		}

		allow(w, review.Request.UID)
	})

	if err := http.ListenAndServe(addr, nil); err != nil {
		panic(err)
	}
}

func allow(w http.ResponseWriter, uid string) {
	resp := &AdmissionReviewResponse{Response: &AdmissionResponse{UID: uid, Allowed: true}}
	write(w, resp)
}

func deny(w http.ResponseWriter, uid string, msg string) {
	resp := &AdmissionReviewResponse{Response: &AdmissionResponse{UID: uid, Allowed: false}}
	resp.Response.Result.Message = msg
	write(w, resp)
}

func write(w http.ResponseWriter, body *AdmissionReviewResponse) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func getenv(name, fallback string) string {
	v := os.Getenv(name)
	if v == "" {
		return fallback
	}
	return v
}
