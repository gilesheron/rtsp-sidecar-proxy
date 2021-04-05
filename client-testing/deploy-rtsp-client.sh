docker build -t ffmpeg-image .

sleep 1

kubectl apply -f rtsp-client.yaml
