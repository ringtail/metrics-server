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

// This file implements a cadvisor datasource, that collects metrics from an instance
// of cadvisor running on a specific host.

package kubetunnel

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"

	"github.com/golang/glog"
	kubelet_client "github.com/kubernetes-incubator/metrics-server/metrics/sources/kubelet/util"
	stats "k8s.io/kubernetes/pkg/kubelet/apis/stats/v1alpha1"
)

const (
	kubeTunnelProxyHost = "X-Tunnel-Proxy-Host"
)

type Host struct {
	IP       string
	Port     int
	Resource string
}

func (h Host) String() string {
	host := fmt.Sprintf("%s:%d", h.IP, h.Port)
	if h.Resource != "" {
		host = fmt.Sprintf("%s/%s", host, h.Resource)
	}
	return host
}

type KubeletClient struct {
	config *kubelet_client.KubeletClientConfig
	client *http.Client
}

type ErrNotFound struct {
	endpoint string
}

func (err *ErrNotFound) Error() string {
	return fmt.Sprintf("%q not found", err.endpoint)
}

func IsNotFoundError(err error) bool {
	_, isNotFound := err.(*ErrNotFound)
	return isNotFound
}

func (self *KubeletClient) postRequestAndGetValue(client *http.Client, req *http.Request, value interface{}) error {
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body - %v", err)
	}
	if response.StatusCode == http.StatusNotFound {
		return &ErrNotFound{req.URL.String()}
	} else if response.StatusCode != http.StatusOK {
		return fmt.Errorf("request failed - %q, response: %q", response.Status, string(body))
	}

	kubeletAddr := "[unknown]"
	if req.URL != nil {
		kubeletAddr = req.URL.Host
	}
	glog.V(10).Infof("Raw response from Kubelet at %s: %s", kubeletAddr, string(body))

	err = json.Unmarshal(body, value)
	if err != nil {
		return fmt.Errorf("failed to parse output. Response: %q. Error: %v", string(body), err)
	}
	return nil
}

func (self *KubeletClient) GetSummary(host Host, nodeName string) (*stats.Summary, error) {
	url := url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", host.IP, host.Port),
		Path:   "/stats/summary/",
	}
	if self.config != nil && self.config.EnableHttps {
		url.Scheme = "https"
	}

	req, err := http.NewRequest("GET", url.String(), nil)
	if err != nil {
		return nil, err
	}
	// add header for kube-tunnel-server
	req.Header.Set(kubeTunnelProxyHost, nodeName)
	summary := &stats.Summary{}
	client := self.client
	if client == nil {
		client = http.DefaultClient
	}
	err = self.postRequestAndGetValue(client, req, summary)
	return summary, err
}

func (self *KubeletClient) GetPort() int {
	return int(self.config.Port)
}

func NewKubeletClient(kubeletConfig *kubelet_client.KubeletClientConfig) (*KubeletClient, error) {
	transport, err := kubelet_client.MakeTransport(kubeletConfig)
	if err != nil {
		return nil, err
	}
	c := &http.Client{
		Transport: transport,
		Timeout:   kubeletConfig.HTTPTimeout,
	}
	return &KubeletClient{
		config: kubeletConfig,
		client: c,
	}, nil
}
