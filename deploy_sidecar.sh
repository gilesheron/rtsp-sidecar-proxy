
# since apply is idempotent, just create the api every deploy 
kubectl apply -f k8s-client.yaml

tar -xf release/rtsp-sidecar-proxy_v0.3.10-6-gda1072a_linux_amd64.tar.gz

docker build -t rtsp-sidecar-proxy .

kubectl apply -f client.yaml

kubectl get pods