# build proxy
make release

# extract proxy binary
tar -xf release/[rtsp-sidecar-proxy]*
docker build -t rtsp-sidecar-proxy .

cd test-images/InitContainer
docker build -t init .

# deploy k8s api handler
cd ../../k8s-api-handler
kubectl apply -f k8s-api.yaml

# start the producer
cd ../producer
./start_stream.sh

# start the consumer
cd ../consumer
./start_consumer.sh