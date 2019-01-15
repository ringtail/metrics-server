package socket

import (
	"github.com/kubernetes-incubator/metrics-server/metrics/core"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

type FakeSocketSink struct {
	core.DataSink
	fakeSocketSinkWriter *FakeSocketSinkWriter
}

type FakeSocketSinkWriter struct {
	SocketSinkWriterInterface
	Points []string
}

func (fssw *FakeSocketSinkWriter) Write(dataPoints []byte) (int, error) {
	fssw.Points = append(fssw.Points, string(dataPoints))
	return 0, nil
}

func (fssw *FakeSocketSinkWriter) Close() {
	//do nothing
}

func NewSocketSink(writer SocketSinkWriterInterface) *SocketSink {
	return &SocketSink{
		Writer: writer,
	}
}

func NewFakeSocketSinkWriter() *FakeSocketSinkWriter {
	return &FakeSocketSinkWriter{
		Points: make([]string, 0),
	}
}

func NewFakeSocketSink() *FakeSocketSink {
	writer := NewFakeSocketSinkWriter()
	return &FakeSocketSink{
		NewSocketSink(writer),
		writer,
	}
}

func TestSocketSinkEmptyInput(t *testing.T) {
	fakeSink := NewFakeSocketSink()
	dataBatch := core.DataBatch{}
	fakeSink.ExportData(&dataBatch)
	assert.Equal(t, 0, len(fakeSink.fakeSocketSinkWriter.Points))
}

func TestSocketSinkMultipleDataInput(t *testing.T) {
	fakeSink := NewFakeSocketSink()
	timestamp := time.Now()

	l := make(map[string]string)
	l["namespace_id"] = "123"
	l["container_name"] = "/system.slice/-.mount"
	l[core.LabelPodId.Key] = "aaaa-bbbb-cccc-dddd"

	l2 := make(map[string]string)
	l2["namespace_id"] = "123"
	l2["container_name"] = "/system.slice/dbus.service"
	l2[core.LabelPodId.Key] = "aaaa-bbbb-cccc-dddd"

	l3 := make(map[string]string)
	l3["namespace_id"] = "123"
	l3[core.LabelPodId.Key] = "aaaa-bbbb-cccc-dddd"

	l4 := make(map[string]string)
	l4["namespace_id"] = ""
	l4[core.LabelPodId.Key] = "aaaa-bbbb-cccc-dddd"

	l5 := make(map[string]string)
	l5["namespace_id"] = "123"
	l5[core.LabelPodId.Key] = "aaaa-bbbb-cccc-dddd"

	metricSet1 := core.MetricSet{
		Labels: l,
		MetricValues: map[string]core.MetricValue{
			"/system.slice/-.mount//cpu/limit": {
				ValueType:  core.ValueInt64,
				MetricType: core.MetricCumulative,
				IntValue:   123456,
			},
		},
	}

	metricSet2 := core.MetricSet{
		Labels: l2,
		MetricValues: map[string]core.MetricValue{
			"/system.slice/dbus.service//cpu/usage": {
				ValueType:  core.ValueInt64,
				MetricType: core.MetricCumulative,
				IntValue:   123456,
			},
		},
	}

	metricSet3 := core.MetricSet{
		Labels: l3,
		MetricValues: map[string]core.MetricValue{
			"test/metric/1": {
				ValueType:  core.ValueInt64,
				MetricType: core.MetricCumulative,
				IntValue:   123456,
			},
		},
	}

	metricSet4 := core.MetricSet{
		Labels: l4,
		MetricValues: map[string]core.MetricValue{
			"test/metric/1": {
				ValueType:  core.ValueInt64,
				MetricType: core.MetricCumulative,
				IntValue:   123456,
			},
		},
	}

	metricSet5 := core.MetricSet{
		Labels: l5,
		MetricValues: map[string]core.MetricValue{
			"removeme": {
				ValueType:  core.ValueInt64,
				MetricType: core.MetricCumulative,
				IntValue:   123456,
			},
		},
	}

	data := core.DataBatch{
		Timestamp: timestamp,
		MetricSets: map[string]*core.MetricSet{
			"pod1": &metricSet1,
			"pod2": &metricSet2,
			"pod3": &metricSet3,
			"pod4": &metricSet4,
			"pod5": &metricSet5,
		},
	}
	fakeSink.ExportData(&data)
	assert.Equal(t, 5, len(fakeSink.fakeSocketSinkWriter.Points))
}
