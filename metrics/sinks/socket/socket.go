package socket

import (
	"github.com/golang/glog"
	influxdb "github.com/influxdata/influxdb/client"
	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
	"github.com/kubernetes-incubator/metrics-server/metrics/core"
	"github.com/pkg/errors"
	"io/ioutil"
	"net/url"
	"sync"
	"time"
)

const (
	MaxSendBatchSize = 300
)

type SocketSink struct {
	Writer   SocketSinkWriterInterface
	Config   *SocketSinkConfig
	SinkLock *sync.RWMutex
	sync.RWMutex
}

func (sink *SocketSink) Name() string {
	return "Socket Sink"
}

func (sink *SocketSink) Stop() {
	//nothing need to do
}

func (sink *SocketSink) ExportData(dataBatch *core.DataBatch) {
	sink.Lock()
	defer sink.Unlock()
	glog.Infof("Start ExportData %d metrics.\n", len(dataBatch.MetricSets))
	dataPoints := make([]influxdb.Point, 0, 0)

	for _, metricSet := range dataBatch.MetricSets {
		for metricName, metricValue := range metricSet.MetricValues {

			var value interface{}
			if core.ValueInt64 == metricValue.ValueType {
				value = metricValue.IntValue
			} else if core.ValueFloat == metricValue.ValueType {
				value = float64(metricValue.FloatValue)
			} else {
				continue
			}

			// Prepare measurement without fields
			fieldName := "value"
			measurementName := metricName

			point := influxdb.Point{
				Measurement: measurementName,
				Tags:        make(map[string]string, len(metricSet.Labels)),
				Fields: map[string]interface{}{
					fieldName: value,
				},
				Time: dataBatch.Timestamp.UTC(),
			}

			for key, value := range metricSet.Labels {
				if value != "" {
					point.Tags[key] = value
				}
			}
			if point.Tags["cluster_name"] == "" {
				point.Tags["cluster_name"] = GetClusterId()
			}

			if point.Tags["user_id"] == "" {
				point.Tags["user_id"] = GetUid()
			}

			if point.Tags["token"] == "" {
				point.Tags["token"] = GetToken()
			}

			dataPoints = append(dataPoints, point)
			if len(dataPoints) >= MaxSendBatchSize {
				sink.SendData(dataPoints)
				dataPoints = make([]influxdb.Point, 0, 0)
			}
		}

		for _, labeledMetric := range metricSet.LabeledMetrics {

			var value interface{}
			if core.ValueInt64 == labeledMetric.ValueType {
				value = labeledMetric.IntValue
			} else if core.ValueFloat == labeledMetric.ValueType {
				value = float64(labeledMetric.FloatValue)
			} else {
				continue
			}

			// Prepare measurement without fields
			fieldName := "value"
			measurementName := labeledMetric.Name

			point := influxdb.Point{
				Measurement: measurementName,
				Tags:        make(map[string]string, len(metricSet.Labels)+len(labeledMetric.Labels)),
				Fields: map[string]interface{}{
					fieldName: value,
				},
				Time: dataBatch.Timestamp.UTC(),
			}

			for key, value := range metricSet.Labels {
				if value != "" {
					point.Tags[key] = value
				}
			}
			for key, value := range labeledMetric.Labels {
				if value != "" {
					point.Tags[key] = value
				}
			}

			if point.Tags["cluster_name"] == "" {
				point.Tags["cluster_name"] = GetClusterId()
			}

			if point.Tags["user_id"] == "" {
				point.Tags["user_id"] = GetUid()
			}

			if point.Tags["token"] == "" {
				point.Tags["token"] = GetToken()
			}

			dataPoints = append(dataPoints, point)
			if len(dataPoints) >= MaxSendBatchSize {
				sink.SendData(dataPoints)
				dataPoints = make([]influxdb.Point, 0, 0)
			}
		}
	}
	if len(dataPoints) > 0 {
		sink.SendData(dataPoints)
	}
}

func (sink *SocketSink) SendData(dataPoints []influxdb.Point) error {
	sink.SinkLock.Lock()
	defer sink.SinkLock.Unlock()
	metrics := make([]telegraf.Metric, 0)
	for _, m := range dataPoints {
		telegraf_metric, err := metric.New(m.Measurement, m.Tags, m.Fields, m.Time)
		if err != nil {
			glog.Warningf("Failed to convert influxdb data points to telegraf metrics,because of %s\n", err.Error())
			continue
		}
		metrics = append(metrics, telegraf_metric)
	}
	r := metric.NewReader(metrics)
	metrics_bytes, err := ioutil.ReadAll(r)
	if err != nil {
		glog.Warningf("Failed to create metrics_bytes,because of %s\n", err.Error())
		return err
	}
	if _, err := sink.Writer.Write(metrics_bytes); err != nil {
		glog.Warningf("The Connection is broken: (%s) and try to reconnect\n", err.Error())
		sink.Writer.Close()
		return err
	}
	glog.Infof("Successful write %d bytes metrics to monitor server\n", len(metrics_bytes))
	time.Sleep(time.Millisecond * 200)
	return nil
}

func CreateSocketSink(uri url.URL) (core.DataSink, error) {
	conf := &SocketSinkConfig{}
	err := conf.BuildConfig(uri)
	if err != nil {
		return nil, err
	}
	opts, err := url.ParseQuery(uri.RawQuery)
	if err != nil {
		return nil, err
	}

	err = applyDefaults(opts)
	if err != nil {
		return nil, err
	}

	sink := &SocketSink{
		Writer: &SocketSinkWriter{
			SocketSinkConfig: SocketSinkConfig{
				Schema: conf.Schema,
				Host:   conf.Host,
			},
		},
		SinkLock: &sync.RWMutex{},
		Config:   conf,
	}
	return sink, nil
}

func validParams(opts url.Values) error {
	if opts.Get("clusterId") == "" {
		return errors.New("clusterId not found.")
	}
	return nil
}

func applyDefaults(opts url.Values) error {
	clusterId := opts.Get("clusterId")
	if opts.Get("public") != "false" {
		err := validParams(opts)
		if err != nil {
			return err
		}
		metaInfoInstance := GetMetaInfoInstance()
		go metaInfoInstance.RefreshInterval(clusterId)
	}
	return nil
}
