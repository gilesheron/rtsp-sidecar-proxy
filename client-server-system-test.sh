# teardown previous setup
./system-test-teardown.sh
clear

# build proxy
make release

# extract proxy binary
tar -xf release/rtsp-sidecar-proxy_v0.3.10-12-gfad9a5a_linux_amd64.tar.gz
docker build -t rtsp-sidecar-proxy .
clear

# deploy k8s api handler
cd k8s-api
./deploy-k8s-api-handler.sh
clear

# deploy proxy server
cd ../server-testing
./deploy-rtsp-server.sh
clear

# deploy ffmpeg sample client
cd ../client-testing
./deploy-rtsp-client.sh
clear