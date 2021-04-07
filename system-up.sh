# build proxy
make release

# extract proxy binary
tar -xf release/[rtsp-sidecar-proxy]*
docker build -t rtsp-sidecar-proxy .

# build init container
cd test-images/InitContainer
docker build -t init .

# deploy k8s api-server handler
cd ../../k8s-api-handler
kubectl apply -f k8s-api.yaml

echo "waiting 4 seconds api container to initialize..."
sleep 4

# start the producer
cd ../producer
./start_stream.sh

sleep 2

# start the consumer
cd ../consumer
./start_consumer.sh