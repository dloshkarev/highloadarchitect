// Package metrics собирает метрики Prometheus кэша ленты, outbox-воркера
// и fanout-консьюмера. Отдаются на GET /metrics.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	histogramBucketsStart  = 0.005
	histogramBucketsFactor = 2
	histogramBucketsCount  = 10
)

// histogramBuckets — общий набор бакетов для duration-гистограмм и размера
// батча: от 5мс до ~2.5с с шагом ×2.
var histogramBuckets = prometheus.ExponentialBuckets(
	histogramBucketsStart, histogramBucketsFactor, histogramBucketsCount,
)

var (
	// FeedCacheHits — чтения ленты, обслуженные из кэша Redis.
	FeedCacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "feed_cache_hit_total",
		Help: "Чтения ленты, обслуженные из кэша Redis.",
	})
	// FeedCacheMisses — чтения ленты, обслуженные из PostgreSQL:
	// за границей кэша, при ошибке кэша или после прогрева пустой ленты.
	FeedCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
		Name: "feed_cache_miss_total",
		Help: "Чтения ленты, обслуженные из PostgreSQL.",
	})
	// FeedWarmTotal — прогревы кэша ленты из БД.
	FeedWarmTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "feed_warm_total",
		Help: "Прогревы кэша ленты из БД.",
	})
	// FeedWarmDuration — длительность прогрева кэша ленты из БД.
	FeedWarmDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "feed_warm_duration_seconds",
		Help:    "Длительность прогрева кэша ленты из БД.",
		Buckets: histogramBuckets,
	})
	// OutboxBatchSize — размер батча outbox-событий за тик воркера.
	OutboxBatchSize = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "outbox_batch_size",
		Help:    "Размер батча outbox-событий за тик воркера.",
		Buckets: histogramBuckets,
	})
	// OutboxLag — число необработанных событий в outbox после тика воркера.
	OutboxLag = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "outbox_lag",
		Help: "Число необработанных событий в outbox после тика воркера.",
	})
	// FanoutRecipients — получатели, которым fanout добавил посты в ленты.
	FanoutRecipients = promauto.NewCounter(prometheus.CounterOpts{
		Name: "fanout_recipients_total",
		Help: "Получатели, которым fanout добавил посты в ленты.",
	})
	// FanoutDuration — длительность обработки батча fanout-консьюмером.
	FanoutDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "fanout_duration_seconds",
		Help:    "Длительность обработки батча fanout-консьюмером.",
		Buckets: histogramBuckets,
	})
)
