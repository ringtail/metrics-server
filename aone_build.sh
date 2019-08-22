#!/usr/bin/env bash

ARCH=$1
REGISTRY=$2
VERSION=$3

if [[ "$ARCH" = "arm64" ]]; then
    # build arm64 image
    DOCKERFILE="deploy/docker/Dockerfile.arm"
#elif [[ "$ARCH" = "sw64" ]]; then
#    # build sw64 image
#    TAG="v1.12.6-aliyun.1"
#    DOCKERFILE="Dockerfile-sw"
else
    # build x86 image
    DOCKERFILE="deploy/docker/Dockerfile"
fi

TAG=$(git log -1 --format=%h)

echo "building image ${REGISTRY}:$VERSION-$TAG-aliyun"
sudo docker build --pull -t ${REGISTRY}:$VERSION-$TAG-aliyun -f ${DOCKERFILE} .

echo "pushing image ${REGISTRY}:$VERSION-$TAG-aliyun"
sudo docker push ${REGISTRY}:$VERSION-$TAG-aliyun