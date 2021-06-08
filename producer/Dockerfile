FROM linuxserver/ffmpeg:latest

WORKDIR /
COPY facemask.mjpeg .

ENTRYPOINT ["ffmpeg", "-re", "-stream_loop", "-1", "-loglevel", "48", "-c:v", "mjpeg", "-i", "facemask.mjpeg", "-c:v", "mjpeg", "-huffman", "0", "-f", "rtsp", "rtsp://127.0.0.2:554/camera"]
