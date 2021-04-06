FROM linuxserver/ffmpeg:latest

WORKDIR /
COPY . .

ENTRYPOINT ["ffmpeg", "-re", "-stream_loop", "-1", "-i", "file.ts", "-c", "copy", "-f", "rtsp", "rtsp://127.0.0.1:554/mystream"]