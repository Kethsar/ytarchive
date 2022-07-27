FROM python:3.8.5-alpine

WORKDIR /go/src

RUN pip install --no-cache-dir --upgrade ffmpeg yt-dlp Enum html

FROM golang:1.16

WORKDIR /go/src

RUN go get github.com/alessio/shellescape
RUN go get github.com/mattn/go-colorable
RUN go get github.com/mattn/go-isatty
RUN go get golang.org/x/net
RUN go get golang.org/x/sys
RUN go get golang.org/x/text
RUN go get golang.org/x/tools

COPY . .

#VOLUME /app/
WORKDIR /go/src

RUN go build -o /go/bin/ytarchiver


VOLUME /go/bin
WORKDIR /go/bin

RUN apk update -f \
    && apk add --no-cache -f \
    ffmpeg \
    && rm -rf /var/cache/apk/*

#RUN ls -la /go \
#    ls -la /go/src \
#    ls -la /go/bin \
#    ls -la /usr/local/src \
#    ls -la /usr/src


#COPY /go/src/ytarchiver /app

#ENV GOROOT=/usr/local/go
#RUN go install ytarchiver





ENTRYPOINT ["/go/bin/ytarchiver"]