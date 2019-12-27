FROM reg.docker.alibaba-inc.com/cos/golang:1.10.8-sw as intermediate
MAINTAINER zhongwei.lzw@alibaba-inc.com

WORKDIR /go/src/github.com/kubernetes-incubator/metrics-server
ADD . /go/src/github.com/kubernetes-incubator/metrics-server
ENV BUILD_TAGS pkd
RUN make build

#cat Dockerfile
FROM reg.docker.alibaba-inc.com/ali/os_sw_7u5

COPY --from=intermediate github.com/kubernetes-incubator/metrics-server/metrics-server /

# nobody:nobody
# USER 65534:65534
ENTRYPOINT ["/metrics-server"]
