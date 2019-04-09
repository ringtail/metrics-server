package socket

import (
	"encoding/json"
	"fmt"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/cs"
	"github.com/denverdino/aliyungo/metadata"
	"github.com/golang/glog"
	influxdb "github.com/influxdata/influxdb/client"
	"github.com/influxdata/telegraf/metric"
	"github.com/pkg/errors"
	"net"
	"net/url"
	"os"
	"time"
)

/**
SocketSinkWriter is a tcp socket writer for telegraf
you can simply use SocketSinkWriter like that:

	--sink=socket:tcp://127.0.0.1:8093
*/

var MetaInfoInstance *MetaInfo

func init() {
	MetaInfoInstance = &MetaInfo{}
}

type SocketSinkWriterInterface interface {
	Write([]byte) (int, error)
	Close()
}

type SocketSinkWriter struct {
	SocketSinkWriterInterface
	Conn            net.Conn
	KeepAlivePeriod time.Duration
	SocketSinkConfig
}

func (writer *SocketSinkWriter) connect() error {
	c, err := net.DialTimeout(writer.Schema, writer.Host, time.Second*3)
	if err != nil {
		return err
	}

	if err := writer.setKeepAlive(c); err != nil {
		glog.Infof("unable to configure keep alive (%s): %s", writer.Host, err)
	}

	writer.Conn = c
	return nil
}

func (writer *SocketSinkWriter) setKeepAlive(c net.Conn) error {
	if &writer.KeepAlivePeriod == nil {
		return nil
	}
	tcpc, ok := c.(*net.TCPConn)
	if !ok {
		return fmt.Errorf("cannot set keep alive on a %s socket", writer.Host)
	}
	if writer.KeepAlivePeriod == 0 {
		return tcpc.SetKeepAlive(false)
	}
	if err := tcpc.SetKeepAlive(true); err != nil {
		return err
	}
	return tcpc.SetKeepAlivePeriod(writer.KeepAlivePeriod)
}

func (writer *SocketSinkWriter) Close() {
	if writer.Conn != nil {
		writer.Conn.Close()
		writer.Conn = nil
	}
}

func (writer *SocketSinkWriter) Write(bs []byte) (int, error) {
	if writer.Conn == nil {
		err := writer.connect()
		if err != nil {
			glog.Warningf("Failed to create connection to socket sink,Because of %v", err)
			return 0, err
		}
	}
	return writer.Conn.Write(bs)
}

type SocketSinkConfig struct {
	Schema string
	Host   string
}

func (config *SocketSinkConfig) BuildConfig(uri url.URL) error {
	if len(uri.Host) > 0 {
		config.Host = uri.Host
	}
	if len(uri.Scheme) > 0 {
		config.Schema = uri.Scheme
	}

	if config.Host == "" || config.Schema == "" {
		return errors.New("BadSocketSinkConfig")
	}

	return nil
}

func serialize(m influxdb.Point) ([]byte, error) {
	tm, err := metric.New(m.Measurement, m.Tags, m.Fields, m.Time)
	if err != nil {
		return nil, err
	}
	return tm.Serialize(), nil
}

const (
	DEFAULT_STS_INTERVAL = time.Minute * 60
	CS_ENDPOINT          = "cs.aliyuncs.com"
	CS_API_VERSION       = "2015-12-15"
)

type Token struct {
	Token string `json:"token"`
}

type MetaInfo struct {
	RegionId  string
	ClusterId string
	Uid       string
	Token     string
}

func (metaInfo *MetaInfo) RefreshInterval(clusterId string) {
	metaInfo.Refresh(clusterId)
	ticker := time.NewTicker(DEFAULT_STS_INTERVAL)
	for {
		select {
		case <-ticker.C:
			metaInfo.Refresh(clusterId)
		}
	}
}

func (metaInfo *MetaInfo) Refresh(clusterId string) {
	m := metadata.NewMetaData(nil)

	var (
		rolename string
		err      error
	)

	if rolename, err = m.RoleName(); err != nil {
		glog.Errorf("Failed to refresh sts rolename,because of %s\n", err.Error())
		os.Exit(-1)
	}

	role, err := m.RamRoleToken(rolename)

	if err != nil {
		glog.Errorf("Failed to refresh sts token,because of %s\n", err.Error())
		os.Exit(-1)
	}

	metaInfo.ClusterId = clusterId

	region, err := m.Region()
	if err != nil {
		// handle
		glog.Errorf("Failed to get regionId,because of %s\n", err.Error())
		return
	}
	metaInfo.RegionId = region

	uid, err := m.OwnerAccountID()
	if err != nil {
		// handle
		glog.Errorf("Failed to get uid,because of %s\n", err.Error())
		return
	}
	metaInfo.Uid = uid

	client, err := cs.NewClientWithStsToken(metaInfo.RegionId, role.AccessKeyId, role.AccessKeySecret, role.SecurityToken)
	if err != nil {
		glog.Errorf("Failed to create sdk client with sts token,because of %s\n", err.Error())
		return
	}

	request := requests.NewCommonRequest()
	request.Domain = CS_ENDPOINT
	request.Version = CS_API_VERSION
	request.ApiName = "DescribeMonitorToken"
	request.PathPattern = fmt.Sprintf("/k8s/%s/monitor/token", metaInfo.ClusterId)

	response, err := client.ProcessCommonRequest(request)
	if err != nil {
		// handle
		glog.Errorf("Failed to refresh token,because of %s\n", err.Error())
		return
	}

	t := &Token{}
	err = json.Unmarshal(response.GetHttpContentBytes(), t)
	if err != nil {
		glog.Errorf("Failed to unmarshal token,because of %s\n", err.Error())
		return
	}
	metaInfo.Token = t.Token
}

func GetMetaInfoInstance() *MetaInfo {
	return MetaInfoInstance
}

func GetToken() string {
	return MetaInfoInstance.Token
}

func GetClusterId() string {
	return MetaInfoInstance.ClusterId
}

func GetUid() string {
	return MetaInfoInstance.Uid
}
