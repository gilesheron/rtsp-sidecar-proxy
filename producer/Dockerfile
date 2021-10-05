FROM linuxserver/ffmpeg:latest

WORKDIR /

ENTRYPOINT ["ffmpeg", "-re", "-loglevel", "40", "-c:v", "mjpeg", "-i", "rtsp://demo:demo@ipvmdemo.dyndns.org:5541/onvif-media/media.amp?profile=profile_1_mjpeg&sessiontimeout=60&streamtype=unicast", "-c:v", "mjpeg", "-huffman", "0", "-f", "rtsp", "rtsp://127.0.0.1:554/camera"]
