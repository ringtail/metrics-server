// Copyright 2014 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:generate ./hooks/run_extpoints.sh

package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/spf13/pflag"

	"github.com/emicklei/go-restful"
	"github.com/kubernetes-incubator/metrics-server/common/flags"
	kube_config "github.com/kubernetes-incubator/metrics-server/common/kubernetes"
	"github.com/kubernetes-incubator/metrics-server/metrics/cmd/heapster-apiserver/app"
	"github.com/kubernetes-incubator/metrics-server/metrics/core"
	"github.com/kubernetes-incubator/metrics-server/metrics/manager"
	"github.com/kubernetes-incubator/metrics-server/metrics/options"
	"github.com/kubernetes-incubator/metrics-server/metrics/processors"
	"github.com/kubernetes-incubator/metrics-server/metrics/sinks"
	metricsink "github.com/kubernetes-incubator/metrics-server/metrics/sinks/metric"
	"github.com/kubernetes-incubator/metrics-server/metrics/sources"
	"github.com/kubernetes-incubator/metrics-server/metrics/util"
	"github.com/kubernetes-incubator/metrics-server/version"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apiserver/pkg/server/healthz"
	"k8s.io/apiserver/pkg/util/flag"
	"k8s.io/apiserver/pkg/util/logs"
	kube_client "k8s.io/client-go/kubernetes"
	v1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"net"
	"strconv"

	"github.com/kubernetes-incubator/metrics-server/metrics/api/v1"
	metricsApi "github.com/kubernetes-incubator/metrics-server/metrics/apis/metrics"
)

func main() {
	opt := options.NewHeapsterRunOptions()
	opt.AddFlags(pflag.CommandLine)

	flag.InitFlags()

	if opt.Version {
		fmt.Println(version.VersionInfo())
		os.Exit(0)
	}

	logs.InitLogs()
	defer logs.FlushLogs()

	setLabelSeperator(opt)
	setMaxProcs(opt)
	glog.Infof(strings.Join(os.Args, " "))
	glog.Infof("Metrics Server version %v", version.MetricsServerVersion)
	if err := validateFlags(opt); err != nil {
		glog.Fatal(err)
	}

	kubernetesUrl, err := getKubernetesAddress(opt.Sources)
	if err != nil {
		glog.Fatalf("Failed to get kubernetes address: %v", err)
	}
	sourceManager := createSourceManagerOrDie(opt.Sources)
	sinkManager, metricSink := createAndInitSinksOrDie(opt.Sinks)

	podLister, nodeLister := getListersOrDie(kubernetesUrl)
	dataProcessors := createDataProcessorsOrDie(kubernetesUrl, podLister)

	man, err := manager.NewManager(sourceManager, dataProcessors, sinkManager,
		opt.MetricResolution, manager.DefaultScrapeOffset, manager.DefaultMaxParallelism)
	if err != nil {
		glog.Fatalf("Failed to create main manager: %v", err)
	}
	man.Start()

	go func() {
		// Run API server
		server, err := app.NewHeapsterApiServer(opt, metricSink, nodeLister, podLister)
		if err != nil {
			glog.Warningf("Could not create the API server: %v", err)
		} else {
			server.AddHealthzChecks(healthzChecker(metricSink))

			glog.Infof("Starting Heapster API server...")
			glog.Fatal(server.RunServer())
		}
	}()

	mux := http.NewServeMux()
	handler := SetupHandlers(metricSink, podLister, nodeLister, nil, opt.DisableMetricExport)
	healthz.InstallHandler(mux, healthzChecker(metricSink))

	addr := net.JoinHostPort(opt.Ip, strconv.Itoa(opt.Port))
	glog.Infof("Starting heapster on port %d", opt.Port)
	mux.Handle("/", handler)
	glog.Fatal(http.ListenAndServe(addr, mux))
}

func createSourceManagerOrDie(src flags.Uris) core.MetricsSource {
	if len(src) != 1 {
		glog.Fatal("Wrong number of sources specified")
	}
	sourceFactory := sources.NewSourceFactory()
	sourceProvider, err := sourceFactory.BuildAll(src)
	if err != nil {
		glog.Fatalf("Failed to create source provide: %v", err)
	}
	sourceManager, err := sources.NewSourceManager(sourceProvider, sources.DefaultMetricsScrapeTimeout)
	if err != nil {
		glog.Fatalf("Failed to create source manager: %v", err)
	}
	return sourceManager
}

func createAndInitSinksOrDie(sinkAddresses flags.Uris) (core.DataSink, *metricsink.MetricSink) {
	sinksFactory := sinks.NewSinkFactory()
	metricSink, sinkList := sinksFactory.BuildAll(sinkAddresses)
	if metricSink == nil {
		glog.Fatal("Failed to create metric sink")
	}
	for _, sink := range sinkList {
		glog.Infof("Starting with %s", sink.Name())
	}
	sinkManager, err := sinks.NewDataSinkManager(sinkList, sinks.DefaultSinkExportDataTimeout, sinks.DefaultSinkStopTimeout)
	if err != nil {
		glog.Fatalf("Failed to create sink manager: %v", err)
	}
	return sinkManager, metricSink
}

func getListersOrDie(kubernetesUrl *url.URL) (v1listers.PodLister, v1listers.NodeLister) {
	kubeClient := createKubeClientOrDie(kubernetesUrl)

	podLister, err := getPodLister(kubeClient)
	if err != nil {
		glog.Fatalf("Failed to create podLister: %v", err)
	}
	nodeLister, _, err := util.GetNodeLister(kubeClient)
	if err != nil {
		glog.Fatalf("Failed to create nodeLister: %v", err)
	}
	return podLister, nodeLister
}

func createKubeClientOrDie(kubernetesUrl *url.URL) *kube_client.Clientset {
	kubeConfig, err := kube_config.GetKubeClientConfig(kubernetesUrl)
	if err != nil {
		glog.Fatalf("Failed to get client config: %v", err)
	}
	return kube_client.NewForConfigOrDie(kubeConfig)
}

func createDataProcessorsOrDie(kubernetesUrl *url.URL, podLister v1listers.PodLister) []core.DataProcessor {
	dataProcessors := []core.DataProcessor{
		// Convert cumulative to rate
		processors.NewRateCalculator(core.RateMetricsMapping),
	}

	podBasedEnricher, err := processors.NewPodBasedEnricher(podLister)
	if err != nil {
		glog.Fatalf("Failed to create PodBasedEnricher: %v", err)
	}
	dataProcessors = append(dataProcessors, podBasedEnricher)

	namespaceBasedEnricher, err := processors.NewNamespaceBasedEnricher(kubernetesUrl)
	if err != nil {
		glog.Fatalf("Failed to create NamespaceBasedEnricher: %v", err)
	}
	dataProcessors = append(dataProcessors, namespaceBasedEnricher)

	// aggregators
	metricsToAggregate := []string{
		core.MetricCpuUsageRate.Name,
		core.MetricMemoryUsage.Name,
		core.MetricCpuRequest.Name,
		core.MetricCpuLimit.Name,
		core.MetricMemoryRequest.Name,
		core.MetricMemoryLimit.Name,
	}

	metricsToAggregateForNode := []string{
		core.MetricCpuRequest.Name,
		core.MetricCpuLimit.Name,
		core.MetricMemoryRequest.Name,
		core.MetricMemoryLimit.Name,
	}

	dataProcessors = append(dataProcessors,
		processors.NewPodAggregator(),
		&processors.NamespaceAggregator{
			MetricsToAggregate: metricsToAggregate,
		},
		&processors.NodeAggregator{
			MetricsToAggregate: metricsToAggregateForNode,
		},
		&processors.ClusterAggregator{
			MetricsToAggregate: metricsToAggregate,
		})

	nodeAutoscalingEnricher, err := processors.NewNodeAutoscalingEnricher(kubernetesUrl)
	if err != nil {
		glog.Fatalf("Failed to create NodeAutoscalingEnricher: %v", err)
	}
	dataProcessors = append(dataProcessors, nodeAutoscalingEnricher)
	return dataProcessors
}

const (
	minMetricsCount = 1
	maxMetricsDelay = 3 * time.Minute
)

func healthzChecker(metricSink *metricsink.MetricSink) healthz.HealthzChecker {
	return healthz.NamedCheck("healthz", func(r *http.Request) error {
		batch := metricSink.GetLatestDataBatch()
		if batch == nil {
			return errors.New("could not get the latest data batch")
		}
		if time.Since(batch.Timestamp) > maxMetricsDelay {
			message := fmt.Sprintf("No current data batch available (latest: %s).", batch.Timestamp.String())
			glog.Warningf(message)
			return errors.New(message)
		}
		if len(batch.MetricSets) < minMetricsCount {
			message := fmt.Sprintf("Not enough metrics found in the latest data batch: %d (expected min. %d) %s", len(batch.MetricSets), minMetricsCount, batch.Timestamp.String())
			glog.Warningf(message)
			return errors.New(message)
		}
		return nil
	})
}

// Gets the address of the kubernetes source from the list of source URIs.
// Possible kubernetes sources are: 'kubernetes' and 'kubernetes.summary_api'
func getKubernetesAddress(args flags.Uris) (*url.URL, error) {
	for _, uri := range args {
		if strings.SplitN(uri.Key, ".", 2)[0] == "kubernetes" {
			return &uri.Val, nil
		}
	}
	return nil, fmt.Errorf("No kubernetes source found.")
}

func getPodLister(kubeClient *kube_client.Clientset) (v1listers.PodLister, error) {
	lw := cache.NewListWatchFromClient(kubeClient.Core().RESTClient(), "pods", corev1.NamespaceAll, fields.Everything())
	store := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	podLister := v1listers.NewPodLister(store)
	reflector := cache.NewReflector(lw, &corev1.Pod{}, store, time.Hour)
	go reflector.Run(wait.NeverStop)
	return podLister, nil
}

func validateFlags(opt *options.HeapsterRunOptions) error {
	if opt.MetricResolution < 5*time.Second {
		return fmt.Errorf("metric resolution needs to be greater than 5 seconds - %d", opt.MetricResolution)
	}
	return nil
}

func setMaxProcs(opt *options.HeapsterRunOptions) {
	// Allow as many threads as we have cores unless the user specified a value.
	var numProcs int
	if opt.MaxProcs < 1 {
		numProcs = runtime.NumCPU()
	} else {
		numProcs = opt.MaxProcs
	}
	runtime.GOMAXPROCS(numProcs)

	// Check if the setting was successful.
	actualNumProcs := runtime.GOMAXPROCS(0)
	if actualNumProcs != numProcs {
		glog.Warningf("Specified max procs of %d but using %d", numProcs, actualNumProcs)
	}
}

func setLabelSeperator(opt *options.HeapsterRunOptions) {
	util.SetLabelSeperator(opt.LabelSeperator)
}

func SetupHandlers(metricSink *metricsink.MetricSink, podLister v1listers.PodLister, nodeLister v1listers.NodeLister, historicalSource core.HistoricalSource, disableMetricExport bool) http.Handler {

	runningInKubernetes := true

	// Make API handler.
	wsContainer := restful.NewContainer()
	wsContainer.EnableContentEncoding(true)
	wsContainer.Router(restful.CurlyRouter{})
	a := v1.NewApi(runningInKubernetes, metricSink, historicalSource, disableMetricExport)
	a.Register(wsContainer)
	// Metrics API
	m := metricsApi.NewApi(metricSink, podLister, nodeLister)
	m.Register(wsContainer)

	return wsContainer
}
