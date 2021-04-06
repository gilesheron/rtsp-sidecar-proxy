# build ffmpeg container
docker build -t ffmpeg-image .

sleep 1

# start stream and proxy
kubectl apply -f camera.yaml