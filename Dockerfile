FROM alpine:latest
WORKDIR /
COPY ./rtsp-sidecar-proxy /usr/bin
ENTRYPOINT ["/usr/bin/rtsp-sidecar-proxy"]
