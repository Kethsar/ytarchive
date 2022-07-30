FROM --platform=${BUILDPLATFORM} golang:1.16 AS build-env

ARG TARGETARCH
ENV BUILDX_ARCH="${TARGETOS:-linux}-${TARGETARCH}"

WORKDIR /go/src

COPY . .

RUN GOOS=linux GOARCH=${TARGETPLATFORM} go build -o ytarchiver

FROM --platform=${BUILDPLATFORM} golang:1.16

WORKDIR /app

COPY --from=build-env /go/src/ytarchiver ./

FROM --platform=linux/amd64 golang:1.16 as stage-amd
RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive \
    apt-get install --no-install-recommends --assume-yes \
      ffmpeg

FROM --platform=linux/arm golang:1.16 as stage-arm
RUN dpkg --add-architecture armhf
RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive \
    apt-get install --no-install-recommends --assume-yes \
      ffmpeg:armhf

FROM --platform=linux/arm64 golang:1.16 as stage-arm64
RUN dpkg --add-architecture arm64
RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive \
    apt-get install --no-install-recommends --assume-yes \
      ffmpeg:arm64

ARG TARGETARCH
FROM stage-${TARGETARCH} as final
WORKDIR /app
RUN mkdir /app/downloads
VOLUME /app/downloads

ENTRYPOINT [ "/app/ytarchiver" ]