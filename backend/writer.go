package backend

import (
	"math"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/prometheus/prometheus/prompb"

	"github.com/wavefronthq/wavefront-sdk-go/senders"
)

type MetricWriter struct {
	prefix       string
	tags         map[string]string
	sender       senders.Sender
	convertPaths bool
	metricsSent  int64
	numErrors    int64
	errorRate    float64
}

var tagValueReplacer = strings.NewReplacer("\"", "\\\"", "*", "-")

func NewMetricWriter(sender senders.Sender, prefix string, tags map[string]string, convertPaths bool) *MetricWriter {
	return &MetricWriter{
		sender:       sender,
		prefix:       prefix,
		tags:         tags,
		convertPaths: convertPaths,
	}
}

func (w *MetricWriter) Write(rq prompb.WriteRequest) {
	for _, ts := range rq.Timeseries {
		w.writeMetrics(&ts)
	}
}

func (w *MetricWriter) writeMetrics(ts *prompb.TimeSeries) {
	tags := make(map[string]string, len(ts.Labels))
	for _, l := range ts.Labels {
		tagName := w.buildTagName(l.Name)
		tags[tagName] = l.Value
	}
	fieldName := w.buildMetricName(tags["__name__"])
	delete(tags, "__name__")
	for _, value := range ts.Samples {
		// Prometheus sometimes sends NaN samples. We interpret them as
		// missing data and simply skip them.
		if math.IsNaN(value.Value) {
			continue
		}
		source, finalTags := w.buildTags(tags)
		err := w.sender.SendMetric(fieldName, value.Value, value.Timestamp, source, finalTags)
		if err != nil {
			log.Warnf("Cannot send metric: %s. Reason: %s. Skipping to next", fieldName, err)
		}
	}
}

func (w *MetricWriter) buildMetricName(name string) string {
	if w.prefix != "" {
		name = w.prefix + "_" + name
	}
	if w.convertPaths {
		name = strings.Replace(name, "_", ".", -1)
	}
	return name
}

func (w *MetricWriter) buildTagName(name string) string {
	if name != "__name__" && w.convertPaths {
		name = strings.Replace(name, "_", ".", -1)
	}
	return name
}

func (w *MetricWriter) buildTags(mTags map[string]string) (string, map[string]string) {
	// Remove all empty tags.
	for k, v := range mTags {
		if v == "" {
			log.Debugf("dropping empty tag %s", k)
			delete(mTags, k)
		}
	}

	source := ""
	if val, ok := mTags["instance"]; ok {
		source = val
		delete(mTags, "instance")
	}

	// Add optional tags
	for k, v := range w.tags {
		mTags[k] = v
	}

	return tagValueReplacer.Replace(source), mTags
}

func (w *MetricWriter) HealthCheck() (int, string) {
	tags := map[string]string{
		"test": "test",
	}
	err := w.sender.SendMetric("prom.gateway.healthcheck", 0, time.Now().Unix(), "", tags)
	if err != nil {
		return 503, err.Error()
	}
	return 200, "OK"
}
