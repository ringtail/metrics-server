#!/bin/bash

./build.sh

echo Testing telegraf ...
gocov test ./... > coverage.json
gocov test github.com/influxdata/telegraf/plugins/outputs/sls > coverage.json
gocov-html coverage.json > coverage.html