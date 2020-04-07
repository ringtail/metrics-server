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
	"encoding/base64"
	"crypto/aes"
	"crypto/cipher"
	"io/ioutil"
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
	ConfigPath           = "/var/addon/token-config"
)

type AKInfo struct {
	AccessKeyId     string `json:"access.key.id"`
	AccessKeySecret string `json:"access.key.secret"`
	SecurityToken   string `json:"security.token"`
	Expiration      string `json:"expiration"`
	Keyring         string `json:"keyring"`
}

func PKCS5UnPadding(origData []byte) []byte {
	length := len(origData)
	unpadding := int(origData[length-1])
	return origData[:(length - unpadding)]
}

func Decrypt(s string, keyring []byte) ([]byte, error) {
	cdata, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		glog.Errorf("failed to decode base64 string, err: %v", err)
		return nil, err
	}
	block, err := aes.NewCipher(keyring)
	if err != nil {
		glog.Errorf("failed to new cipher, err: %v", err)
		return nil, err
	}
	blockSize := block.BlockSize()

	iv := cdata[:blockSize]
	blockMode := cipher.NewCBCDecrypter(block, iv)
	origData := make([]byte, len(cdata)-blockSize)

	blockMode.CryptBlocks(origData, cdata[blockSize:])

	origData = PKCS5UnPadding(origData)
	return origData, nil
}

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

	var akInfo AKInfo
	if _, err := os.Stat(ConfigPath); err == nil {
		//获取token config json
		encodeTokenCfg, err := ioutil.ReadFile(ConfigPath)
		if err != nil {
			glog.Fatalf("failed to read token config, err: %v", err)
		}
		err = json.Unmarshal(encodeTokenCfg, &akInfo)
		if err != nil {
			glog.Fatalf("error unmarshal token config: %v", err)
		}
		keyring := akInfo.Keyring
		ak, err := Decrypt(akInfo.AccessKeyId, []byte(keyring))
		if err != nil {
			glog.Fatalf("failed to decode ak, err: %v", err)
		}

		sk, err := Decrypt(akInfo.AccessKeySecret, []byte(keyring))
		if err != nil {
			glog.Fatalf("failed to decode sk, err: %v", err)
		}

		token, err := Decrypt(akInfo.SecurityToken, []byte(keyring))
		if err != nil {
			glog.Fatalf("failed to decode token, err: %v", err)
		}
		glog.Infof("ak %s sk %s token %s", string(ak), string(sk), string(token))
		layout := "2006-01-02T15:04:05Z"
		t, err := time.Parse(layout, akInfo.Expiration)
		if err != nil {
			fmt.Errorf(err.Error())
		}
		if t.Before(time.Now()) {
			glog.Errorf("invalid token which is expired")
		}
		akInfo.AccessKeyId = string(ak)
		akInfo.AccessKeySecret = string(sk)
		akInfo.SecurityToken = string(token)
	} else {
		akInfo.AccessKeyId = role.AccessKeyId
		akInfo.AccessKeySecret = role.AccessKeySecret
		akInfo.SecurityToken = role.SecurityToken
	}
	client, err := cs.NewClientWithStsToken(metaInfo.RegionId, akInfo.AccessKeyId, akInfo.AccessKeySecret, akInfo.SecurityToken)
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
