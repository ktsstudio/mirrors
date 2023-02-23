package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	MirrorSyncCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mirrors_sync_total",
			Help: "Number of successful mirror syncs",
		},
		[]string{
			"mirror",
			"source_type",
			"destination_type",
		},
	)

	MirrorNSCurrentCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "mirrors_ns_current_count",
			Help: "Number of namespaces to which a secret has been successfully mirrored",
		},
		[]string{
			"mirror",
			"source_type",
		},
	)

	VaultLeaseRenewOkCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mirrors_vault_lease_renew_ok_total",
			Help: "Number of successful lease renewals",
		},
		[]string{
			"mirror",
			"vault",
		},
	)

	VaultLeaseRenewErrorCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mirrors_vault_lease_renew_error_total",
			Help: "Number of errored lease renewals",
		},
		[]string{
			"mirror",
			"vault",
			"http_code",
		},
	)
)

func init() {
	metrics.Registry.MustRegister(
		MirrorSyncCount,
		MirrorNSCurrentCount,
		VaultLeaseRenewOkCount,
		VaultLeaseRenewErrorCount,
	)
}
