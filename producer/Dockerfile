FROM linuxserver/ffmpeg:latest

WORKDIR /
COPY . .

ENTRYPOINT ["ffmpeg", "-re", "-stream_loop", "-1", "-loglevel", "48", "-i", "file.ts", "-c", "copy", "-f", "rtsp", "rtsp://127.0.0.2:554/camera"]
