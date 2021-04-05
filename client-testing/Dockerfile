FROM linuxserver/ffmpeg:latest

WORKDIR /
COPY . .

ENTRYPOINT ["ffmpeg", "-re", "-stream_loop", "-1", "-i", "file.ts", "-max_muxing_queue_size",  "512", "-c", "copy", "-f", "rtsp", "rtsp://10.96.2.2:554/mystream"]
