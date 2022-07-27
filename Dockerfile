FROM golang:1.16 AS build-env

WORKDIR /go/src

COPY . .

RUN go build -o /go/bin/ytarchiver

WORKDIR /app

RUN apt-get -y update
RUN apt-get install -y ffmpeg

FROM golang:1.16

WORKDIR /app

VOLUME /app

COPY --from=build-env ./go/bin/ytarchiver .

ENTRYPOINT ["/app/ytarchiver"]
