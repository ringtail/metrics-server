### install dependence
gdm restore

> 如果本地环境没有 gdm，需要执行  go get github.com/sparrc/gdm 进行安装

### godep save dependence
godep save github.com/influxdata/telegraf/cmd/telegraf

### compile
godep go build

### compile 

go build cmd/telegraf/telegraf.go

### run Telegraf 

./telegraf -config ~/telegraf.conf -test

