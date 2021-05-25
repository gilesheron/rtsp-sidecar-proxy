# rtsp-sidecar-proxy

_rtsp-sidecar-proxy_ is a simple, ready-to-use and zero-dependency RTSP proxy.  It was forked from [aler9/rtsp-simple-proxy](https://github.com/aler9/rtsp-simple-proxy) and then reworked into a sidecar proxy which connects to RTSP sources on-demand rather than through configuration.

Features:

* Receive multiple streams in TCP or UDP
* Distribute streams in TCP or UDP
* Supports the RTP/RTCP streaming protocol
* Supports authentication (i.e. username and password)
* Designed to be used as a sidecar proxy in Kubernetes

## Installation

Docker images are posted to dockerhub

## Usage

Add this container to any pod that needs to use the proxy.

Also add the msm-init container as an init container.

## Links

Related projects

* https://github.com/aler9/rtsp-sidecar-proxy
* https://github.com/aler9/rtsp-simple-server
* https://github.com/aler9/gortsplib
* https://github.com/media-streaming-mesh/msm-init

IETF Standards

* RTSP 1.0 https://tools.ietf.org/html/rfc2326
* RTSP 2.0 https://tools.ietf.org/html/rfc7826
* HTTP 1.1 https://tools.ietf.org/html/rfc2616
