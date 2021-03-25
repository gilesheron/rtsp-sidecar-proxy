tar -xf release/rtsp-sidecar-proxy_v0.3.10-6-gda1072a_linux_amd64.tar.gz

docker build -t rtsp-sidecar-proxy .

kubectl apply -f client.yaml

kubectl get pods