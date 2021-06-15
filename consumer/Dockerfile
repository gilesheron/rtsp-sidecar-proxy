FROM linuxserver/ffmpeg:latest

WORKDIR /

ENTRYPOINT ["ffmpeg", "-re", "-stream_loop", "-1", "-loglevel", "40", "-c:v", "mjpeg", "-i", "rtsp://10.96.3.3:554/facemask", "-c:v", "h264", "-f", "rtsp", "rtsp://127.0.0.2:554/faceview"]
