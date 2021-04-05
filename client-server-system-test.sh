# extract proxy binary
tar -xf release/rtsp-sidecar-proxy_v0.3.10-10-gebb29d9_linux_amd64.tar.gz

# deploy k8s api handler
cd k8s-api
./deploy-k8s-api-handler.sh

# deploy proxy server
cd ../server-testing
./deploy-rtsp-server.sh

# deploy ffmpeg sample client
cd ../client-testing
./deploy-rtsp-client.sh