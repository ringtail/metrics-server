FROM golang:1.9.6-alpine3.6

COPY metrics-server /
RUN apk add --no-cache tzdata

# nobody:nobody
# USER 65534:65534
ENTRYPOINT ["/metrics-server"]
