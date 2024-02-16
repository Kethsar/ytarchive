# build the application in a separate container to keep the final image small
FROM golang:1.22 AS builder

COPY . /app
WORKDIR /app
RUN GO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o ytarchive .

# setup the resulting image using the same base as the golang image
FROM debian:bookworm

# ffmpeg (5.1.4) from stable does not work properly so we have to install v6 from unstable
# we copy config files for apt to add unstable as a source and pin all packages to stable
COPY ./etc /etc
# we also need to install ca-certificates to avoid SSL errors
RUN apt-get update \
    && apt-get --no-install-recommends -y install ca-certificates \
    && apt-get -t sid --no-install-recommends -y install ffmpeg \
    && rm -rf /var/lib/apt/lists/*

# copy the application over from the builder container
COPY --from=builder /app/ytarchive /usr/local/bin

# switch to a non-root user
RUN useradd -m ytarchive
USER ytarchive

VOLUME [ "/output" ]
WORKDIR /output

# we use bash to allow for environment variable expansion in docker-compose files
# and to avoid issues with parantheses in the output format
ENTRYPOINT [ "/bin/bash", "-c" ]
CMD [ "ytarchive -h" ]