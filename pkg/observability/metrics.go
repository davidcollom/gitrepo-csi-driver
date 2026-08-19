package observability

import (
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	MountRequests     *prometheus.CounterVec
	MountDuration     *prometheus.HistogramVec
	CloneDuration     *prometheus.HistogramVec
	CacheHits         *prometheus.CounterVec
	CacheMisses       *prometheus.CounterVec
	PolicyDenials     *prometheus.CounterVec
	RepositorySize    *prometheus.GaugeVec
	RepositoryFiles   *prometheus.GaugeVec
	SubmoduleRequests *prometheus.CounterVec
	CredentialErrors  *prometheus.CounterVec
	once              sync.Once
}

func NewMetrics() *Metrics {
	return &Metrics{
		MountRequests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "gitcontent_mount_requests_total", Help: "Mount request count"}, []string{"namespace", "policy", "result"}),
		MountDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "gitcontent_mount_duration_seconds", Help: "Mount duration", Buckets: prometheus.DefBuckets}, []string{"namespace", "policy", "result"}),
		CloneDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "gitcontent_clone_duration_seconds", Help: "Clone duration", Buckets: prometheus.DefBuckets}, []string{"host", "result"}),
		CacheHits: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "gitcontent_cache_hits_total", Help: "Cache hits"}, []string{"host"}),
		CacheMisses: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "gitcontent_cache_misses_total", Help: "Cache misses"}, []string{"host"}),
		PolicyDenials: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "gitcontent_policy_denials_total", Help: "Policy denials"}, []string{"namespace", "policy", "reason"}),
		RepositorySize: prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "gitcontent_repository_size_bytes", Help: "Repository size"}, []string{"host"}),
		RepositoryFiles: prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "gitcontent_repository_files_total", Help: "Repository file count"}, []string{"host"}),
		SubmoduleRequests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "gitcontent_submodule_requests_total", Help: "Submodule requests"}, []string{"namespace", "policy", "result"}),
		CredentialErrors: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "gitcontent_credentials_errors_total", Help: "Credential errors"}, []string{"profile", "reason"}),
	}
}

func (m *Metrics) Register() {
	m.once.Do(func() {
		prometheus.MustRegister(m.MountRequests, m.MountDuration, m.CloneDuration, m.CacheHits, m.CacheMisses, m.PolicyDenials, m.RepositorySize, m.RepositoryFiles, m.SubmoduleRequests, m.CredentialErrors)
	})
}

func (m *Metrics) Handler() http.Handler {
	m.Register()
	return promhttp.Handler()
}

func (m *Metrics) ObserveMount(namespace, policy, result string, duration time.Duration) {
	m.MountRequests.WithLabelValues(namespace, policy, result).Inc()
	m.MountDuration.WithLabelValues(namespace, policy, result).Observe(duration.Seconds())
}
