FROM linuxserver/ffmpeg:latest

WORKDIR /
COPY facemask.mjpeg .

ENTRYPOINT ["ffmpeg", "-re", "-stream_loop", "-1", "-loglevel", "40", "-c:v", "mjpeg", "-i", "facemask.mjpeg", "-c:v", "mjpeg", "-huffman", "0", "-f", "rtsp", "rtsp://127.0.0.1:554/camera"]
