sudo docker buildx build --platform linux/arm64 -f Dockerfile_nostream -t ryusid/motion-mqtt-mapper . --push
