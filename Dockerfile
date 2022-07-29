FROM golang:1.16 AS build-env

WORKDIR /go/src

COPY . .

RUN go build -o ytarchiver

FROM golang:1.16

WORKDIR /app

COPY --from=build-env /go/src/ytarchiver ./

RUN apt-get -y update && \
    apt-get install -y ffmpeg

RUN mkdir /app/downloads
VOLUME /app/downloads

ENTRYPOINT [ "/app/ytarchiver" ]