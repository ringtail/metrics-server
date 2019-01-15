#!/bin/bash

cd $GOPATH/src/github.com/influxdata/telegraf

export GOPATH=$GOPATH/src/github.com/influxdata/telegraf/vendor:$GOPATH

export CGO_ENABLED=0
export GOARCH="amd64"
export GOOS="linux"
go build cmd/telegraf/telegraf.go

