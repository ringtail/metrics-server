FROM golang:alpine3.12 AS build-env
ADD . /src/github.com/kubernetes-incubator/metrics-server
ENV GOPATH /:/src/github.com/kubernetes-incubator/metrics-server/vendor
WORKDIR /src/github.com/kubernetes-incubator/metrics-server
RUN apt-get update -y && apt-get install gcc ca-certificates -y
RUN make


FROM golang:alpine3.12
COPY --from=build-env /usr/local/go/lib/time/zoneinfo.zip /usr/local/go/lib/time/zoneinfo.zip
COPY --from=build-env /src/github.com/kubernetes-incubator/metrics-server/metrics-server /
COPY --from=build-env /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
# nobody:nobody
# USER 65534:65534
ENTRYPOINT ["/metrics-server"]

