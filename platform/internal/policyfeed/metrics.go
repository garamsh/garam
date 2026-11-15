package policyfeed

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/garamsh/garam/lib/errorx"
)

// The metrics the feed's standing is read from on `/metrics`.
const (
	// MarksMetric is the outbox's marks by state: `pending`, which the
	// next pass reads, and `set_aside`, which the service refused for
	// good and no pass reads.
	MarksMetric = "garam_policy_feed_outbox_marks"
	// LastSuccessMetric is the Unix time of the last pass of this
	// replica's loop that ended without a failure. It is absent until
	// the first one.
	LastSuccessMetric = "garam_policy_feed_last_success_timestamp_seconds"
)

// scrapeTimeout bounds the outbox read of one scrape.
const scrapeTimeout = 5 * time.Second

// Collector reports the feed's standing at each scrape: the marks are
// counted from the outbox, so every replica reports the whole feed, and
// the last success is this replica's loop's own.
type Collector struct {
	store Store
	// last is nil where the process runs no loop.
	last        func() time.Time
	marks, pass *prometheus.Desc
}

// NewCollector constructs the collector and registers it on
// registerer. last reports when this replica's loop last succeeded, and
// is nil where the replica delivers nothing, which leaves that metric
// out.
func NewCollector(
	registerer prometheus.Registerer, store Store, last func() time.Time,
) error {
	c := &Collector{
		store: store,
		last:  last,
		marks: prometheus.NewDesc(MarksMetric,
			"Marks in the policy feed's outbox, by whether a pass reads them.",
			[]string{"state"}, nil),
		pass: prometheus.NewDesc(LastSuccessMetric,
			"Unix time of the last policy feed delivery pass that did not fail.",
			nil, nil),
	}
	if err := registerer.Register(c); err != nil {
		return errorx.Wrap(err, "register policy feed collector", errorx.KindInternal)
	}
	return nil
}

// Describe sends the descriptors of the metrics.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.marks
	ch <- c.pass
}

// Collect reads the outbox and sends the metrics. A read that fails is
// sent as the error of the scrape, so it does not read as an empty
// outbox.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), scrapeTimeout)
	defer cancel()
	standing, err := c.store.Standing(ctx)
	if err != nil {
		ch <- prometheus.NewInvalidMetric(c.marks, err)
	} else {
		ch <- prometheus.MustNewConstMetric(c.marks, prometheus.GaugeValue,
			float64(standing.Pending), "pending")
		ch <- prometheus.MustNewConstMetric(c.marks, prometheus.GaugeValue,
			float64(standing.SetAside), "set_aside")
	}
	if c.last == nil {
		return
	}
	if at := c.last(); !at.IsZero() {
		ch <- prometheus.MustNewConstMetric(c.pass, prometheus.GaugeValue,
			float64(at.UnixNano())/float64(time.Second))
	}
}
