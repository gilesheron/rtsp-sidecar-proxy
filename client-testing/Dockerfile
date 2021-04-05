FROM linuxserver/ffmpeg:latest

WORKDIR /
COPY . .

ENTRYPOINT ["ffmpeg", "-re", "-stream_loop", "-1", "-i", "file.ts", "-c", "copy", "-f", "rtsp", "rtsp://10.96.2.2:554/mystream"]
