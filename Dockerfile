ARG base_image=alpine:latest
ARG builder_image=concourse/golang-builder

FROM ${builder_image} as builder
WORKDIR /src

COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . ./
ENV CGO_ENABLED 0
RUN go build -o /proxy ./cmd/proxy

FROM ${base_image} AS resource
# Ensure /etc/hosts is honored
# https://github.com/golang/go/issues/22846
# https://github.com/gliderlabs/docker-alpine/issues/367
RUN echo "hosts: files dns" > /etc/nsswitch.conf
COPY --from=builder /proxy /proxy

ENV MONGO_URI host.docker.internal
ENV RABBITMQ_HOST host.docker.internal
ENTRYPOINT [ "/proxy" ]
